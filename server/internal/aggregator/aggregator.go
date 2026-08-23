package aggregator

import (
	"context"
	"sync"
	"time"

	"promonitor/server/internal/metrics"
	"promonitor/server/internal/store"
)

const windowSize = 10 * time.Minute

// bucketKey 把任意测量时间映射到其所属 10 分钟桶的起点（unix 秒）
func bucketKey(ts int64) int64 {
	return (ts / int64(windowSize.Seconds())) * int64(windowSize.Seconds())
}

// window 是单个被控的某个 10 分钟聚合窗口（进程内内存，不落库）
type window struct {
	start     time.Time
	n         int
	cpuSum    float64
	memSum    float64
	diskSum   float64
	netInSum  float64
	netOutSum float64
	pingSum   []float64
	pingCnt   []int
	name      string
	ip        string
	mu        sync.Mutex
}

// Aggregator 管理所有被控的聚合窗口，并定时结算落库
type Aggregator struct {
	store  *store.Store
	mu     sync.Mutex
	wins   map[string]map[int64]*window // serverID -> 桶起点 -> 窗口
	pingN  int
}

func New(s *store.Store, pingNodes int) *Aggregator {
	return &Aggregator{store: s, wins: make(map[string]map[int64]*window), pingN: pingNodes}
}

// Add 收到一条高频样本（ts 为被控测量时间，unix 秒），累加到对应桶（不落库）
func (a *Aggregator) Add(smpl metrics.Sample) {
	// 样本时间非法（<=0）则回退到服务端接收时间，保证不强退
	ts := smpl.TS
	if ts <= 0 {
		ts = time.Now().Unix()
	}
	bucket := bucketKey(ts)

	a.mu.Lock()
	serverWins, ok := a.wins[smpl.ServerID]
	if !ok {
		serverWins = make(map[int64]*window)
		a.wins[smpl.ServerID] = serverWins
		// 首次上报即注册被控（一次/窗口，开销极低），使其立即出现在列表中
		go func(id, name, ip string) {
			_ = a.store.UpsertServer(context.Background(), id, name, ip)
		}(smpl.ServerID, smpl.Name, smpl.IP)
	}
	w, ok := serverWins[bucket]
	if !ok {
		w = &window{start: time.Unix(bucket, 0), pingSum: make([]float64, a.pingN), pingCnt: make([]int, a.pingN)}
		serverWins[bucket] = w
	}
	a.mu.Unlock()

	w.mu.Lock()
	w.n++
	w.cpuSum += smpl.CPU
	w.memSum += smpl.Mem
	w.diskSum += smpl.Disk
	w.netInSum += smpl.NetIn
	w.netOutSum += smpl.NetOut
	w.name = smpl.Name
	w.ip = smpl.IP
	for i := 0; i < a.pingN && i < len(smpl.Pings); i++ {
		v := smpl.Pings[i]
		if v >= 0 && v <= 1000 { // >1000ms 视为无效，不计入
			w.pingSum[i] += v
			w.pingCnt[i]++
		}
	}
	w.mu.Unlock()
}

// FlushDue 扫描并结算所有"已结束"的 10 分钟窗口（即 bucket 终点已过当前时间）。
// 桶起点即数据时间（来自被控测量时间），天然适配网络故障导致的积压数据。
func (a *Aggregator) FlushDue(ctx context.Context) {
	now := time.Now()
	threshold := now.Add(-windowSize) // 仅结算 1 个完整窗口之前的桶，避免未采满就落库

	a.mu.Lock()
	for id, serverWins := range a.wins {
		for bucket, w := range serverWins {
			if time.Unix(bucket, 0).Add(windowSize).After(threshold) {
				continue // 该桶尚未结束，保留
			}
			w.mu.Lock()
			avg := metrics.AggRow{
				ServerID: id,
				TS:       bucket, // 桶起点 = 测量时间对齐，作为数据时间
				CPU:      w.cpuSum / float64(w.n),
				Mem:      w.memSum / float64(w.n),
				Disk:     w.diskSum / float64(w.n),
				NetIn:    w.netInSum / float64(w.n),
				NetOut:   w.netOutSum / float64(w.n),
				Pings:    make([]float64, a.pingN),
			}
			for i := range avg.Pings {
				avg.Pings[i] = -1 // 哨兵：未采集/超时
			}
			for i := 0; i < a.pingN; i++ {
				if w.pingCnt[i] > 0 {
					avg.Pings[i] = w.pingSum[i] / float64(w.pingCnt[i])
				}
			}
			name, ip := w.name, w.ip
			delete(serverWins, bucket) // 已结算，移除该桶
			w.mu.Unlock()

			// 持久化异步进行，避免阻塞定时器
			go func(id string, avg metrics.AggRow, name, ip string) {
				_ = a.store.UpsertServer(ctx, id, name, ip)
				if err := a.store.InsertAgg(ctx, avg); err != nil {
					return
				}
				_ = a.store.UpdateSnapshot(ctx, avg)
			}(id, avg, name, ip)
		}
	}
	a.mu.Unlock()
}

// Start 启动后台定时结算（每 30s 扫描一次）
func (a *Aggregator) Start(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
				a.FlushDue(ctx)
			}
		}
	}()
}
