package metrics

import "time"

// Sample 是被控 Agent 上报的单次原始样本（高频，不落库）
type Sample struct {
	ServerID string    `json:"server_id"`
	Name     string    `json:"name"`
	IP       string    `json:"ip"`
	TS       int64     `json:"ts"` // unix 秒（被控测量时间，主控以此为数据时间）
	CPU      float64   `json:"cpu"`
	Mem      float64   `json:"mem"`
	Disk     float64   `json:"disk"`
	NetIn    float64   `json:"net_in"`
	NetOut   float64   `json:"net_out"`
	Pings    []float64 `json:"pings"` // 各节点延迟(ms)，无效为 null 或负数
}

// AggRow 是 10 分钟窗口结算后的聚合行（落库 & 接口返回）
type AggRow struct {
	ServerID string    `json:"server_id"`
	TS       int64     `json:"ts"` // unix 秒
	CPU      float64   `json:"cpu_avg"`
	Mem      float64   `json:"mem_avg"`
	Disk     float64   `json:"disk_avg"`
	NetIn    float64   `json:"net_in_avg"`
	NetOut   float64   `json:"net_out_avg"`
	Pings    []float64 `json:"ping_nodes"`
}

// ServerView 是列表/详情返回的被控视图（含最新快照）
type ServerView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	IP        string    `json:"ip"`
	Status    int       `json:"status"`
	CPU       *float64  `json:"cpu"`
	Mem       *float64  `json:"mem"`
	Disk      *float64  `json:"disk"`
	NetIn     *float64  `json:"net_in"`
	NetOut    *float64  `json:"net_out"`
	Pings     []float64 `json:"pings"`
	UpdatedAt time.Time `json:"updated_at"`
}
