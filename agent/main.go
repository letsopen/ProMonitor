package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gn "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// Sample 是上报给主控的单次样本（结构与主控 metrics.Sample 对应）
type Sample struct {
	ServerID string    `json:"server_id"`
	Name     string    `json:"name"`
	IP       string    `json:"ip"`
	TS       int64     `json:"ts"` // unix 秒（测量时间）
	CPU      float64   `json:"cpu"`
	Mem      float64   `json:"mem"`
	Disk     float64   `json:"disk"`
	NetIn    float64   `json:"net_in"`
	NetOut   float64   `json:"net_out"`
	Pings    []float64 `json:"pings"`
}

// PingNode 是主控统一下发的探测节点
type PingNode struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	IP   string `json:"ip"`   // 仅 IPv4
	Port int    `json:"port"` // TCP 探测端口
}

// PingConfig 是主控下发的网络探测配置
type PingConfig struct {
	Type  string     `json:"type"` // "icmp" | "tcp"
	Nodes []PingNode `json:"nodes"`
}

// AgentConfig 是被控运行参数
type AgentConfig struct {
	Master string
	Secret string
	SID    string
	Name   string
	IP     string
}

const (
	maxQueue   = 500   // 积压队列上限，超限丢最旧，避免 OOM
	maxRetries = 5     // 单条样本最大重试次数
	baseBackoff = 2 * time.Second
	maxBackoff  = 60 * time.Second
	configTTL  = 5 * time.Minute // 多久重新拉取一次探测配置
)

var (
	prevNet gn.IOCountersStat
	prevTS  time.Time
	haveP   bool
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func measureTCPPing(host string, port int, timeout time.Duration) float64 {
	addr := net.JoinHostPort(host, itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return -1 // 无效（含超时）
	}
	conn.Close()
	return float64(time.Since(start).Milliseconds())
}

// measureICMPPing 用 raw socket 做 ICMP echo，需要容器具备 CAP_NET_RAW（--cap-add NET_RAW）或特权模式。
// 否则会失败，调用方应回退到 -1。
func measureICMPPing(host string, timeout time.Duration) float64 {
	// 使用 net 包建立 ICMP（ip4:icmp）连接；非特权环境会失败
	c, err := net.DialTimeout("ip4:icmp", host, timeout)
	if err != nil {
		return -1
	}
	defer c.Close()
	// 构造最小 ICMP echo 请求（type=8, code=0, id, seq, 校验和=0 简化）
	id := uint16(os.Getpid() & 0xffff)
	seq := uint16(1)
	msg := make([]byte, 8)
	msg[0] = 8 // Echo Request
	msg[1] = 0
	msg[2] = 0
	msg[3] = 0
	msg[4] = byte(id >> 8)
	msg[5] = byte(id)
	msg[6] = byte(seq >> 8)
	msg[7] = byte(seq)

	_ = c.SetDeadline(time.Now().Add(timeout))
	start := time.Now()
	if _, err := c.Write(msg); err != nil {
		return -1
	}
	buf := make([]byte, 256)
	if _, err := c.Read(buf); err != nil {
		return -1
	}
	return float64(time.Since(start).Milliseconds())
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Sender 持有一个带重试的积压队列：采集结果先入队，后台顺序出队上传。
type Sender struct {
	cfg    AgentConfig
	client *http.Client
	queue  []*Sample
	mu     sync.Mutex
	cond   *sync.Cond
	stop   chan struct{}
}

func NewSender(cfg AgentConfig) *Sender {
	s := &Sender{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		stop:   make(chan struct{}),
	}
	s.cond = sync.NewCond(&s.mu)
	go s.loop()
	return s
}

// Enqueue 把样本放入积压队列（非阻塞，仅在队列满时丢弃最旧）
func (s *Sender) Enqueue(smpl *Sample) {
	s.mu.Lock()
	if len(s.queue) >= maxQueue {
		// 丢弃最旧的一条，腾出空间
		s.queue = s.queue[1:]
	}
	s.queue = append(s.queue, smpl)
	s.cond.Signal()
	s.mu.Unlock()
}

// loop 后台 goroutine：顺序取出样本，上传失败按指数退避重试
func (s *Sender) loop() {
	for {
		s.mu.Lock()
		for len(s.queue) == 0 {
			select {
			case <-s.stop:
				s.mu.Unlock()
				return
			default:
			}
			s.cond.Wait()
			select {
			case <-s.stop:
				s.mu.Unlock()
				return
			default:
			}
		}
		smpl := s.queue[0]
		s.mu.Unlock()

		if s.sendWithRetry(smpl) {
			s.mu.Lock()
			// 仅当仍是最前面那条时才出队（防止并发下错位）
			if len(s.queue) > 0 && s.queue[0] == smpl {
				s.queue = s.queue[1:]
			}
			s.mu.Unlock()
		}
		// 失败则保留在队首，进入退避后重试（在 sendWithRetry 内已实现退避）
	}
}

// sendWithRetry 上传单条样本，失败指数退避重试，成功返回 true
func (s *Sender) sendWithRetry(smpl *Sample) bool {
	body, err := json.Marshal(smpl)
	if err != nil {
		return true // 序列化失败无法恢复，直接丢弃
	}
	mac := hmac.New(sha256.New, []byte(s.cfg.Secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	url := strings.TrimRight(s.cfg.Master, "/") + "/api/ingest"
	for attempt := 0; attempt < maxRetries; attempt++ {
		req, err := http.NewRequest("POST", url, bytes.NewReader(body))
		if err != nil {
			log.Printf("build request error: %v", err)
			return true
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Signature", sig)

		resp, err := s.client.Do(req)
		if err != nil {
			log.Printf("send error (attempt %d/%d): %v", attempt+1, maxRetries, err)
		} else {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return true
			}
			log.Printf("send rejected (attempt %d/%d): status %d", attempt+1, maxRetries, resp.StatusCode)
		}
		// 退避（最后一步不再 sleep）
		if attempt < maxRetries-1 {
			backoff := baseBackoff * time.Duration(1<<uint(attempt))
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			time.Sleep(backoff)
		}
	}
	log.Printf("give up sample ts=%d after %d attempts (dropped)", smpl.TS, maxRetries)
	return true // 达到最大重试后丢弃，避免永久阻塞队列
}

func (s *Sender) Stop() {
	close(s.stop)
}

// fetchPingConfig 从主控拉取探测配置
func fetchPingConfig(master string) (PingConfig, error) {
	var pc PingConfig
	resp, err := http.Get(strings.TrimRight(master, "/") + "/api/ping-config")
	if err != nil {
		return pc, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return pc, errHTTPStatus(resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&pc); err != nil {
		return pc, err
	}
	return pc, nil
}

type errHTTPStatus int

func (e errHTTPStatus) Error() string { return "unexpected status " + itoa(int(e)) }

func collect(cfg AgentConfig, pc PingConfig) *Sample {
	now := time.Now()

	cpuPct, _ := cpu.Percent(0, false)
	var cpuVal float64
	if len(cpuPct) > 0 {
		cpuVal = cpuPct[0]
	}

	vm, _ := mem.VirtualMemory()
	memVal := 0.0
	if vm != nil {
		memVal = vm.UsedPercent
	}

	du, _ := disk.Usage("/")
	diskVal := 0.0
	if du != nil {
		diskVal = du.UsedPercent
	}

	var netIn, netOut float64
	if cs, err := gn.IOCounters(false); err == nil && len(cs) > 0 {
		c := cs[0]
		if haveP && !prevTS.IsZero() {
			dt := now.Sub(prevTS).Seconds()
			if dt > 0 {
				netIn = float64(c.BytesRecv-prevNet.BytesRecv) / dt
				netOut = float64(c.BytesSent-prevNet.BytesSent) / dt
			}
		}
		prevNet = c
		prevTS = now
		haveP = true
	}

	pings := make([]float64, len(pc.Nodes))
	for i, n := range pc.Nodes {
		switch pc.Type {
		case "icmp":
			pings[i] = measureICMPPing(n.IP, 1*time.Second)
		default: // tcp：使用节点自身配置的端口
			pings[i] = measureTCPPing(n.IP, n.Port, 1*time.Second)
		}
	}

	return &Sample{
		ServerID: cfg.SID,
		Name:     cfg.Name,
		IP:       cfg.IP,
		TS:       now.Unix(), // 测量时间，unix 秒
		CPU:      cpuVal,
		Mem:      memVal,
		Disk:     diskVal,
		NetIn:    netIn,
		NetOut:   netOut,
		Pings:    pings,
	}
}

func main() {
	master := flag.String("master", env("MASTER_URL", "http://localhost:9000"), "主控地址")
	secret := flag.String("secret", env("HMAC_SECRET", ""), "HMAC 预共享密钥")
	sid := flag.String("id", env("SERVER_ID", ""), "被控唯一ID")
	name := flag.String("name", env("SERVER_NAME", ""), "展示名(默认同ID)")
	ip := flag.String("ip", env("SERVER_IP", ""), "被控IP")
	// -targets 仅作为可选覆盖：若设置则忽略主控下发的节点
	targets := flag.String("targets", env("PING_TARGETS", ""), "可选：逗号分隔节点，覆盖主控下发")
	interval := flag.Duration("interval", 30*time.Second, "采集间隔")
	flag.Parse()

	if *secret == "" {
		log.Fatal("HMAC_SECRET is required")
	}
	if *sid == "" {
		log.Fatal("SERVER_ID is required")
	}
	if *name == "" {
		*name = *sid
	}

	cfg := AgentConfig{Master: *master, Secret: *secret, SID: *sid, Name: *name, IP: *ip}

	// 本地回退节点：把逗号分隔的 host 转成 PingNode（TCP 用默认端口 80）
	localNodes := func() []PingNode {
		var out []PingNode
		for _, h := range splitTargets(*targets, 50) {
			out = append(out, PingNode{Name: h, IP: h, Port: 80})
		}
		return out
	}

	// 初始拉取探测配置；失败则用本地覆盖或空节点
	pc := PingConfig{Type: "tcp"}
	npc, err := fetchPingConfig(*master)
	if err != nil {
		log.Printf("warn: fetch ping-config failed (%v); fallback to local targets", err)
		pc.Nodes = localNodes()
	} else {
		log.Printf("ping-config: type=%s nodes=%d", npc.Type, len(npc.Nodes))
		if len(npc.Nodes) == 0 && *targets != "" {
			npc.Nodes = localNodes()
		}
		pc = npc
	}

	sender := NewSender(cfg)
	defer sender.Stop()

	// 配置快照用 atomic.Value 承载，供采集循环无锁读取、刷新 goroutine 安全更新
	var cfgStore atomic.Value
	cfgStore.Store(pc)

	// 周期性重新拉取配置（热更新），并刷新探测节点
	go func() {
		t := time.NewTicker(configTTL)
		defer t.Stop()
		for range t.C {
			if npc, err := fetchPingConfig(*master); err == nil {
				if len(npc.Nodes) == 0 && *targets != "" {
					npc.Nodes = localNodes()
				}
				cfgStore.Store(npc)
				log.Printf("ping-config refreshed: type=%s nodes=%d", npc.Type, len(npc.Nodes))
			}
		}
	}()

	ticker := time.NewTicker(*interval)
	collectAndSend := func() {
		sender.Enqueue(collect(cfg, cfgStore.Load().(PingConfig)))
	}
	collectAndSend()
	for range ticker.C {
		collectAndSend()
	}
}

func splitTargets(s string, max int) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	if len(parts) > max {
		parts = parts[:max]
	}
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
