package metrics

import "time"

// PingNode 是主控统一管理的延迟探测节点
type PingNode struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	IP   string `json:"ip"`   // 仅 IPv4
	Port int    `json:"port"` // TCP 探测端口
}

// PingConfigView 是被控拉取的网络探测配置（主控统一下发）
type PingConfigView struct {
	Type  string     `json:"type"` // "icmp" | "tcp"
	Nodes []PingNode `json:"nodes"`
}

// Sample 是被控 Agent 上报的单次原始样本（高频，不落库）
type Sample struct {
	ServerID  string    `json:"server_id"`
	Name      string    `json:"name"`
	IP        string    `json:"ip"`
	TS        int64     `json:"ts"` // unix 秒（被控测量时间，主控以此为数据时间）
	CPU       float64   `json:"cpu"`
	Mem       float64   `json:"mem"`
	Disk      float64   `json:"disk"`
	NetIn     float64   `json:"net_in"`
	NetOut    float64   `json:"net_out"`
	CPUCores  int       `json:"cpu_cores"`
	MemTotal  uint64    `json:"mem_total_mb"`
	DiskTotal uint64    `json:"disk_total_gb"`
	Pings     []float64 `json:"pings"` // 各节点延迟(ms)，无效为 null 或负数
}

// AggRow 是 10 分钟窗口结算后的聚合行（落库 & 接口返回）
// 其中 CPUCores/MemTotal/DiskTotal 仅用于实时快照刷新，历史聚合行中可留空。
type AggRow struct {
	ServerID  string    `json:"server_id"`
	TS        int64     `json:"ts"` // unix 秒
	CPU       float64   `json:"cpu_avg"`
	Mem       float64   `json:"mem_avg"`
	Disk      float64   `json:"disk_avg"`
	NetIn     float64   `json:"net_in_avg"`
	NetOut    float64   `json:"net_out_avg"`
	CPUCores  *int      `json:"cpu_cores,omitempty"`
	MemTotal  *uint64   `json:"mem_total_mb,omitempty"`
	DiskTotal *uint64   `json:"disk_total_gb,omitempty"`
	Pings     []float64 `json:"ping_nodes"`
}

// HistoryPayload 是被控端按整数 10 分钟边界聚合后，批量上报到 /api/history 的负载。
// 其中 cpu/mem/disk 为占用百分比，net_in/net_out 为 KB/s，pings 为各节点平均延迟（ms）。
type HistoryPayload struct {
	ServerID string  `json:"server_id"`
	Name     string  `json:"name"`
	IP       string  `json:"ip"`
	Rows     []AggRow `json:"rows"`
}

// ServerView 是列表/详情返回的被控视图（含最新快照）
type ServerView struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	IP         string    `json:"ip"`
	Status     int       `json:"status"`
	CPU        *float64  `json:"cpu"`
	Mem        *float64  `json:"mem"`
	Disk       *float64  `json:"disk"`
	NetIn      *float64  `json:"net_in"`
	NetOut     *float64  `json:"net_out"`
	CPUCores   *int      `json:"cpu_cores"`
	MemTotal   *uint64   `json:"mem_total_mb"`
	DiskTotal  *uint64   `json:"disk_total_gb"`
	Pings      []float64 `json:"pings"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// PingTimeoutMs 是探测失败（-1 哨兵）在前端展示时统一映射成的延迟值。
// 正常 ping 全球任意节点都不会达到这个量级，因此用高位值让超时曲线明显居于顶部，
// 同时保证曲线连续性（不会出现断点）。
const PingTimeoutMs = 9999.0

// NormalizePing 把单个探测值转换为前端展示值：
//   - 失败哨兵 -1 → PingTimeoutMs（超时）
//   - 其余（>=0，含 0ms 合法值）原样返回
//
// 数据库仍存储原始 -1，此映射仅在返回给前端前执行，避免污染历史数据。
func NormalizePing(v float64) float64 {
	if v < 0 {
		return PingTimeoutMs
	}
	return v
}

// NormalizePings 对整个数组做 NormalizePing 映射，返回新切片（不修改入参）。
func NormalizePings(in []float64) []float64 {
	if len(in) == 0 {
		return in
	}
	out := make([]float64, len(in))
	for i, v := range in {
		out[i] = NormalizePing(v)
	}
	return out
}
