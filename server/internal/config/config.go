package config

import (
	"os"
	"strconv"
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
	// PingType 延迟探测方式："icmp" | "tcp"（默认 tcp）。
	// 探测节点清单不再由环境变量管理，而是存数据库 ping_nodes 表，
	// 通过管理后台维护、/api/ping-config 下发给被控。
	PingType string
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

	pingType := get("PING_TYPE", "tcp")
	if pingType != "icmp" && pingType != "tcp" {
		pingType = "tcp"
	}

	return &Config{
		Port:          port,
		DBPath:        get("DB_PATH", "./promonitor.db"),
		HMACSecret:    os.Getenv("HMAC_SECRET"),
		AdminUser:     get("ADMIN_USER", "admin"),
		AdminPass:     os.Getenv("ADMIN_PASS"),
		SessionSecret: get("SESSION_SECRET", "change-me-session-secret"),
		FrontendDir:   get("FRONTEND_DIR", "./dist"),
		PingType:      pingType,
	}
}
