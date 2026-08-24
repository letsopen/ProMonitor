package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	gn "github.com/shirou/gopsutil/v4/net"
	_ "modernc.org/sqlite"
)

// 常量定义
type envKey string

const (
	defaultCollectInterval = 30 * time.Second
	minCollectInterval       = 30 * time.Second
	windowSize               = 10 * time.Minute
	configTTL                = 5 * time.Minute
	maxHistoryRetries        = 5
	baseBackoff              = 2 * time.Second
	maxBackoff               = 60 * time.Second

	// 网速合法性上限：100 Gbps。换算：100Gbps = 1e11 bit/s = 1.25e10 B/s，÷1024 得 KB/s。
	maxNetRateKBps = 100.0 * 1e9 / 8 / 1024 // ≈ 1.2207e7 KB/s
	netRetryDelay  = 3 * time.Second        // 采集失效后重试间隔
	netMaxRetries  = 3                        // 采集失效后最多重试次数
)

// Sample 是实时上报给主控的单次样本（高频，不落库）。
// CPU、Mem、Disk 为百分比；NetIn/NetOut 为 KB/s。
type Sample struct {
	ServerID  string    `json:"server_id"`
	Name      string    `json:"name"`
	IP        string    `json:"ip"`
	TS        int64     `json:"ts"`
	CPU       float64   `json:"cpu"`
	Mem       float64   `json:"mem"`
	Disk      float64   `json:"disk"`
	NetIn     float64   `json:"net_in"`
	NetOut    float64   `json:"net_out"`
	Pings     []float64 `json:"pings"`
	CPUCores  int       `json:"cpu_cores"`    // 实时链路新增：CPU 核心数
	MemTotal  uint64    `json:"mem_total_mb"` // 实时链路新增：内存总大小 MB
	DiskTotal uint64    `json:"disk_total_gb"`// 实时链路新增：磁盘总大小 GB
}

// AggRow 是 10 分钟窗口聚合后的单条记录，对应服务端 metrics_agg 表。
type AggRow struct {
	TS    int64     `json:"ts"`
	CPU   float64   `json:"cpu_avg"`
	Mem   float64   `json:"mem_avg"`
	Disk  float64   `json:"disk_avg"`
	NetIn float64   `json:"net_in_avg"`
	NetOut float64  `json:"net_out_avg"`
	Pings []float64 `json:"ping_nodes"`
}

// HistoryPayload 是被控批量上报给主控 /api/history 的负载。
type HistoryPayload struct {
	ServerID string   `json:"server_id"`
	Name     string   `json:"name"`
	IP       string   `json:"ip"`
	Rows     []AggRow `json:"rows"`
}

// PingNode 是主控统一下发的探测节点
type PingNode struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

// PingConfig 是主控下发的网络探测配置
type PingConfig struct {
	Type  string     `json:"type"`
	Nodes []PingNode `json:"nodes"`
}

// AgentConfig 是被控运行参数
type AgentConfig struct {
	Master          string
	Secret          string
	SID             string
	Name            string
	IP              string
	CollectInterval time.Duration
	Debug           bool
}

// 全局变量
var (
	prevNetMap map[string]gn.IOCountersStat // 每个物理网卡的上次读数，用于按接口求速率差
	prevTS     time.Time
	havePrev   bool
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func parseCollectInterval(s string) time.Duration {
	if s == "" {
		return defaultCollectInterval
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 30 || n%30 != 0 {
		log.Printf("warn: COLLECT_INTERVAL invalid (%q), fallback to 30s", s)
		return defaultCollectInterval
	}
	return time.Duration(n) * time.Second
}

func measureTCPPing(host string, port int, timeout time.Duration) float64 {
	addr := net.JoinHostPort(host, itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return -1
	}
	conn.Close()
	return float64(time.Since(start).Milliseconds())
}

// measureICMPPing 用 raw socket 做 ICMP echo，需要容器具备 CAP_NET_RAW（--cap-add NET_RAW）或特权模式。
func measureICMPPing(host string, timeout time.Duration) float64 {
	c, err := net.DialTimeout("ip4:icmp", host, timeout)
	if err != nil {
		return -1
	}
	defer c.Close()
	id := uint16(os.Getpid() & 0xffff)
	seq := uint16(1)
	msg := make([]byte, 8)
	msg[0] = 8
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

// RealtimeSender 负责实时样本的上报（失败不重试）。
type RealtimeSender struct {
	cfg    AgentConfig
	client *http.Client
}

func NewRealtimeSender(cfg AgentConfig) *RealtimeSender {
	return &RealtimeSender{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}}
}

func (s *RealtimeSender) Send(smpl *Sample) {
	body, _ := json.Marshal(smpl)
	mac := hmac.New(sha256.New, []byte(s.cfg.Secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	url := strings.TrimRight(s.cfg.Master, "/") + "/api/ingest"
	if s.cfg.Debug {
		log.Printf("[DEBUG] >>> realtime ingest body:\n%s", prettyJSON(body))
	}
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", sig)
	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("realtime send error ts=%d: %v", smpl.TS, err)
		return
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if s.cfg.Debug {
		log.Printf("[DEBUG] <<< realtime ingest status=%d body:\n%s", resp.StatusCode, prettyJSON(respBody))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("realtime send rejected ts=%d: status %d", smpl.TS, resp.StatusCode)
	}
}

// HistoryManager 负责本地 10 分钟窗口聚合、持久化与重试上传。
type HistoryManager struct {
	cfg    AgentConfig
	client *http.Client
	store  *HistoryStore
	mu     sync.Mutex
	n      int
	cpuSum float64
	memSum float64
	diskSum float64
	netInSum float64
	netOutSum float64
	pingSum  []float64
	pingCnt  []int
	name     string
	ip       string
}

func NewHistoryManager(cfg AgentConfig, store *HistoryStore, pingN int) *HistoryManager {
	return &HistoryManager{
		cfg:     cfg,
		client:  &http.Client{Timeout: 10 * time.Second},
		store:   store,
		pingSum: make([]float64, pingN),
		pingCnt: make([]int, pingN),
	}
}

// Add 把一次实时样本累加到当前 10 分钟窗口。
func (h *HistoryManager) Add(smpl *Sample) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	h.cpuSum += smpl.CPU
	h.memSum += smpl.Mem
	h.diskSum += smpl.Disk
	h.netInSum += smpl.NetIn
	h.netOutSum += smpl.NetOut
	h.name = smpl.Name
	h.ip = smpl.IP
	// 节点列表可能动态变化，确保 ping 累加数组长度与样本一致。
	if len(smpl.Pings) != len(h.pingSum) {
		newSum := make([]float64, len(smpl.Pings))
		newCnt := make([]int, len(smpl.Pings))
		for i := 0; i < len(h.pingSum) && i < len(smpl.Pings); i++ {
			newSum[i] = h.pingSum[i]
			newCnt[i] = h.pingCnt[i]
		}
		h.pingSum = newSum
		h.pingCnt = newCnt
	}
	for i := 0; i < len(h.pingSum) && i < len(smpl.Pings); i++ {
		v := smpl.Pings[i]
		if v >= 0 && v <= 1000 {
			h.pingSum[i] += v
			h.pingCnt[i]++
		}
	}
}

// Flush 结算当前窗口，生成 AggRow，清空窗口，并持久化到本地待上传表。
// ts 应为当前整数 10 分钟边界。
func (h *HistoryManager) Flush(ts int64) *AggRow {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n == 0 {
		return nil
	}
	row := AggRow{
		TS:     ts,
		CPU:    h.cpuSum / float64(h.n),
		Mem:    h.memSum / float64(h.n),
		Disk:   h.diskSum / float64(h.n),
		NetIn:  h.netInSum / float64(h.n),
		NetOut: h.netOutSum / float64(h.n),
		Pings:  make([]float64, len(h.pingSum)),
	}
	for i := range row.Pings {
		if h.pingCnt[i] > 0 {
			row.Pings[i] = h.pingSum[i] / float64(h.pingCnt[i])
		} else {
			row.Pings[i] = -1
		}
	}
	// 重置窗口
	h.n = 0
	h.cpuSum, h.memSum, h.diskSum, h.netInSum, h.netOutSum = 0, 0, 0, 0, 0
	h.pingSum = make([]float64, len(h.pingSum))
	h.pingCnt = make([]int, len(h.pingCnt))
	return &row
}

// UploadHistory 批量上传聚合记录到主控 /api/history，返回是否成功。
func (h *HistoryManager) UploadHistory(rows []AggRow) bool {
	if len(rows) == 0 {
		return true
	}
	payload := HistoryPayload{
		ServerID: h.cfg.SID,
		Name:     h.cfg.Name,
		IP:       h.cfg.IP,
		Rows:     rows,
	}
	body, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(h.cfg.Secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	url := strings.TrimRight(h.cfg.Master, "/") + "/api/history"
	if h.cfg.Debug {
		log.Printf("[DEBUG] >>> history upload body:\n%s", prettyJSON(body))
	}
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", sig)
	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("history upload error: %v", err)
		return false
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if h.cfg.Debug {
		log.Printf("[DEBUG] <<< history upload status=%d body:\n%s", resp.StatusCode, prettyJSON(respBody))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("history upload rejected: status %d", resp.StatusCode)
		return false
	}
	return true
}

// HistoryStore 是 agent 本地 SQLite，用于持久化待上传的历史聚合记录。
type HistoryStore struct {
	db *sql.DB
}

// OpenHistoryStore 打开本地数据库。
func OpenHistoryStore(path string) (*HistoryStore, error) {
	if path == "" {
		path = "agent.db"
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	schema := `
CREATE TABLE IF NOT EXISTS pending_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id   TEXT NOT NULL,
    ts          INTEGER NOT NULL,
    payload     TEXT NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pending_history_ts ON pending_history(server_id, ts ASC);
`
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &HistoryStore{db: db}, nil
}

func (s *HistoryStore) Close() error { return s.db.Close() }

// SavePending 把一条聚合记录序列化后存入本地。
func (s *HistoryStore) SavePending(serverID string, row AggRow) error {
	payload, _ := json.Marshal(row)
	_, err := s.db.Exec(
		`INSERT INTO pending_history(server_id, ts, payload, created_at) VALUES(?,?,?,?)`,
		serverID, row.TS, string(payload), time.Now().Unix())
	return err
}

// ListPending 读取所有待上传记录。
func (s *HistoryStore) ListPending(serverID string) ([]PendingHistory, error) {
	rows, err := s.db.Query(
		`SELECT id, ts, payload, attempts FROM pending_history WHERE server_id=? ORDER BY ts ASC`,
		serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingHistory
	for rows.Next() {
		var p PendingHistory
		if err := rows.Scan(&p.ID, &p.TS, &p.Payload, &p.Attempts); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePending 删除已上传记录。
func (s *HistoryStore) DeletePending(id int64) error {
	_, err := s.db.Exec(`DELETE FROM pending_history WHERE id=?`, id)
	return err
}

// IncrementAttempts 增加重试次数。
func (s *HistoryStore) IncrementAttempts(id int64) error {
	_, err := s.db.Exec(`UPDATE pending_history SET attempts = attempts + 1 WHERE id=?`, id)
	return err
}

// PendingHistory 是本地待上传记录的表结构。
type PendingHistory struct {
	ID       int64
	TS       int64
	Payload  string
	Attempts int
}

// fetchPingConfig 从主控拉取探测配置
func fetchPingConfig(master string, debug bool) (PingConfig, error) {
	var pc PingConfig
	resp, err := http.Get(strings.TrimRight(master, "/") + "/api/ping-config")
	if err != nil {
		return pc, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if debug {
		log.Printf("[DEBUG] <<< ping-config status=%d body:\n%s", resp.StatusCode, prettyJSON(body))
	}
	if resp.StatusCode != http.StatusOK {
		return pc, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &pc); err != nil {
		return pc, err
	}
	return pc, nil
}

// prettyJSON 尝试将 JSON 美化缩进，失败则返回原始字符串。
func prettyJSON(b []byte) string {
	if len(b) == 0 {
		return "(empty)"
	}
	var out bytes.Buffer
	if json.Indent(&out, b, "", "  ") != nil {
		return string(b)
	}
	return out.String()
}

// isPhysicalNIC 判断是否为物理网卡：排除回环 lo 以及常见虚拟/桥接/容器/隧道接口。
// 部署目标为 Linux/Alpine，接口命名遵循 Linux 惯例；虚拟接口按名称前缀排除。
func isPhysicalNIC(name string) bool {
	lower := strings.ToLower(name)
	if lower == "lo" {
		return false
	}
	for _, p := range []string{
		"docker", "veth", "br-", "virbr", "tun", "tap", "bond",
		"vmnet", "cni", "flannel", "calico", "wg", "dummy", "kube",
		"vboxnet", "ovs", "pods", "containers",
	} {
		if strings.HasPrefix(lower, p) {
			return false
		}
	}
	return true
}

// netRateStatus 表示单次网速采样的结果状态。
type netRateStatus int

const (
	netWarmup netRateStatus = iota // 尚无上一基准，预热中
	netValid
	netInvalid
)

// netRateOnce 读一次全网卡计数器，仅对物理网卡求上下行速率（KB/s）。
// 无论本次是否可用，都会把当前读数写入 prevNetMap 作为下一轮基准：
// 这样网卡重置/计数器回绕后，下一轮（或重试）的 delta 落在极短窗口内，自然回到合法范围，
// 避免了上一版实现中 uint64 相减下溢产生的离谱速率。
func netRateOnce(now time.Time) (netIn, netOut float64, st netRateStatus) {
	cs, err := gn.IOCounters(true)
	if err != nil || len(cs) == 0 {
		return 0, 0, netInvalid
	}
	var dt float64
	if havePrev && !prevTS.IsZero() {
		dt = now.Sub(prevTS).Seconds()
	}
	var din, dout int64
	for _, c := range cs {
		if !isPhysicalNIC(c.Name) {
			continue
		}
		if p, ok := prevNetMap[c.Name]; ok && dt > 0 {
			din += int64(c.BytesRecv) - int64(p.BytesRecv)
			dout += int64(c.BytesSent) - int64(p.BytesSent)
		}
		prevNetMap[c.Name] = c
	}
	// 清理已消失的物理网卡，避免陈旧基准参与后续 delta
	for name := range prevNetMap {
		found := false
		for _, c := range cs {
			if c.Name == name {
				found = true
				break
			}
		}
		if !found {
			delete(prevNetMap, name)
		}
	}
	prevTS = now
	if !havePrev || dt <= 0 {
		havePrev = true
		return 0, 0, netWarmup
	}
	netIn = float64(din) / dt / 1024 // KB/s
	netOut = float64(dout) / dt / 1024
	if netIn >= 0 && netOut >= 0 && netIn <= maxNetRateKBps && netOut <= maxNetRateKBps {
		return netIn, netOut, netValid
	}
	return netIn, netOut, netInvalid
}

// collectNet 采集物理网卡上下行速率（KB/s）。
// 速率落在 [0, 100Gbps] 之外视为采集数据失效，自动重试采集（delay 3s，最多 3 次）；
// 预热阶段（首轮无基准）或重试耗尽则返回 0，绝不把离谱值上报到主控。
func collectNet(now time.Time) (netIn, netOut float64) {
	in, out, st := netRateOnce(now)
	switch st {
	case netValid:
		return in, out
	case netWarmup:
		return 0, 0 // 首轮无基准，直接给 0，不重试
	default: // netInvalid
	}
	for i := 0; i < netMaxRetries; i++ {
		time.Sleep(netRetryDelay)
		// 重试时基准已在上一轮刷新，这里用真实当前时间使 delta 落在 3s 窗口内
		in, out, st = netRateOnce(time.Now())
		switch st {
		case netValid:
			return in, out
		case netWarmup:
			return 0, 0
		}
	}
	return 0, 0 // 重试耗尽，放弃本次网速
}

func collect(cfg AgentConfig, pc PingConfig, now time.Time) *Sample {
	cpuPct, _ := cpu.Percent(0, false)
	var cpuVal float64
	if len(cpuPct) > 0 {
		cpuVal = cpuPct[0]
	}

	vm, _ := mem.VirtualMemory()
	var memVal float64
	var memTotal uint64
	if vm != nil {
		memVal = vm.UsedPercent
		memTotal = vm.Total / 1024 / 1024 // MB
	}

	du, _ := disk.Usage("/")
	var diskVal float64
	var diskTotal uint64
	if du != nil {
		diskVal = du.UsedPercent
		diskTotal = du.Total / 1024 / 1024 / 1024 // GB
	}

	netIn, netOut := collectNet(now)

	pings := make([]float64, len(pc.Nodes))
	for i, n := range pc.Nodes {
		switch pc.Type {
		case "icmp":
			pings[i] = measureICMPPing(n.IP, 1*time.Second)
		default:
			pings[i] = measureTCPPing(n.IP, n.Port, 1*time.Second)
		}
	}

	return &Sample{
		ServerID:  cfg.SID,
		Name:      cfg.Name,
		IP:        cfg.IP,
		TS:        now.Unix(),
		CPU:       cpuVal,
		Mem:       memVal,
		Disk:      diskVal,
		NetIn:     netIn,
		NetOut:    netOut,
		Pings:     pings,
		CPUCores:  runtime.NumCPU(),
		MemTotal:  memTotal,
		DiskTotal: diskTotal,
	}
}

func main() {
	master := flag.String("master", env("MASTER_URL", "http://localhost:9000"), "主控地址")
	secret := flag.String("secret", env("HMAC_SECRET", ""), "HMAC 预共享密钥")
	sid := flag.String("id", env("SERVER_ID", ""), "被控唯一ID")
	name := flag.String("name", env("SERVER_NAME", ""), "展示名(默认同ID)")
	ip := flag.String("ip", env("SERVER_IP", ""), "被控IP")
	interval := flag.Duration("interval", parseCollectInterval(os.Getenv("COLLECT_INTERVAL")), "采集间隔")
	debugStr := strings.ToLower(strings.TrimSpace(env("DEBUG", "true")))
	debug := debugStr != "false" && debugStr != "0" && debugStr != "off"
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
	if *interval < minCollectInterval || int(interval.Seconds())%30 != 0 {
		log.Printf("warn: interval %v invalid, fallback to 30s", *interval)
		*interval = defaultCollectInterval
	}

	cfg := AgentConfig{Master: *master, Secret: *secret, SID: *sid, Name: *name, IP: *ip, CollectInterval: *interval, Debug: debug}
	prevNetMap = make(map[string]gn.IOCountersStat)

	pc := PingConfig{Type: "tcp"}
	if npc, err := fetchPingConfig(*master, cfg.Debug); err == nil {
		pc = npc
		log.Printf("ping-config: type=%s nodes=%d", pc.Type, len(pc.Nodes))
	} else {
		log.Printf("warn: fetch ping-config failed (%v); use tcp empty nodes", err)
	}

	store, err := OpenHistoryStore(env("DB_PATH", "agent.db"))
	if err != nil {
		log.Fatalf("open local store failed: %v", err)
	}
	defer store.Close()

	rtSender := NewRealtimeSender(cfg)
	histMgr := NewHistoryManager(cfg, store, len(pc.Nodes))

	var cfgStore atomic.Value
	cfgStore.Store(pc)

	go func() {
		t := time.NewTicker(configTTL)
		defer t.Stop()
		for range t.C {
			if npc, err := fetchPingConfig(*master, cfg.Debug); err == nil {
				cfgStore.Store(npc)
				log.Printf("ping-config refreshed: type=%s nodes=%d", npc.Type, len(npc.Nodes))
			}
		}
	}()

	// 历史数据重传后台循环
	go func() {
		for {
			histMgr.retryPending()
			time.Sleep(30 * time.Second)
		}
	}()

	// 10 分钟窗口聚合调度：对齐到整数 10 分钟边界
	go func() {
		var lastBucket int64 = -1
		t := time.NewTicker(cfg.CollectInterval)
		defer t.Stop()
		for now := range t.C {
			smpl := collect(cfg, cfgStore.Load().(PingConfig), now)
			histMgr.Add(smpl)
			go rtSender.Send(smpl)

			// 检查是否跨越整数 10 分钟边界
			currentBucket := now.Truncate(windowSize).Unix()
			if lastBucket != -1 && currentBucket != lastBucket {
				if row := histMgr.Flush(lastBucket); row != nil {
					if err := store.SavePending(cfg.SID, *row); err != nil {
						log.Printf("save pending history failed: %v", err)
					}
				}
			}
			lastBucket = currentBucket
		}
	}()

	// 初始采集一次
	smpl := collect(cfg, pc, time.Now())
	histMgr.Add(smpl)
	go rtSender.Send(smpl)

	select {}
}

// retryPending 尝试上传本地积压的历史聚合记录（按 ts 升序批量）。
func (h *HistoryManager) retryPending() {
	pending, err := h.store.ListPending(h.cfg.SID)
	if err != nil {
		log.Printf("list pending history failed: %v", err)
		return
	}
	batchSize := 50
	for i := 0; i < len(pending); i += batchSize {
		end := i + batchSize
		if end > len(pending) {
			end = len(pending)
		}
		var rows []AggRow
		var ids []int64
		for _, p := range pending[i:end] {
			var row AggRow
			if err := json.Unmarshal([]byte(p.Payload), &row); err != nil {
				_ = h.store.DeletePending(p.ID)
				continue
			}
			rows = append(rows, row)
			ids = append(ids, p.ID)
		}
		if len(rows) == 0 {
			continue
		}
		if h.UploadHistory(rows) {
			for _, id := range ids {
				_ = h.store.DeletePending(id)
			}
		} else {
			for _, id := range ids {
				_ = h.store.IncrementAttempts(id)
			}
		}
	}
}
