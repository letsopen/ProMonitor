package config

import (
	"os"
	"strconv"
	"strings"
)

// Config 持有运行时所需环境变量
type Config struct {
	Port          string
	DBPath        string // SQLite 文件路径，默认 ./promonitor.db
	HMACSecret    string
	AdminUser     string
	AdminPass     string
	SessionSecret string
	FrontendDir   string
	// Ping 配置：由主控统一管理，所有被控通过 /api/ping-config 拉取
	PingMethod string   // "icmp" | "tcp"，默认 tcp
	PingPort   int      // TCP 探测端口，默认 80（method=tcp 时生效）
	PingNodes  []string // 探测节点清单，最多 50 个
}

func Load() *Config {
	get := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	port := get("PORT", "9000")
	_, _ = strconv.Atoi(port) // 仅做基本校验，非数字由 ListenAndServe 报错

	method := get("PING_METHOD", "tcp")
	if method != "icmp" && method != "tcp" {
		method = "tcp"
	}
	pingPort, _ := strconv.Atoi(get("PING_PORT", "80"))
	if pingPort <= 0 || pingPort > 65535 {
		pingPort = 80
	}
	nodes := splitNodes(get("PING_NODES", ""), 50)

	return &Config{
		Port:          port,
		DBPath:        get("DB_PATH", "./promonitor.db"),
		HMACSecret:    os.Getenv("HMAC_SECRET"),
		AdminUser:     get("ADMIN_USER", "admin"),
		AdminPass:     os.Getenv("ADMIN_PASS"),
		SessionSecret: get("SESSION_SECRET", "change-me-session-secret"),
		FrontendDir:   get("FRONTEND_DIR", "./dist"),
		PingMethod:    method,
		PingPort:      pingPort,
		PingNodes:     nodes,
	}
}

// splitNodes 解析逗号分隔的节点清单，去空并截断上限
func splitNodes(s string, max int) []string {
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
