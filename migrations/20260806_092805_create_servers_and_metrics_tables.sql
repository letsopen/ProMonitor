-- ============================================
-- 服务器性能监控 - 本地 PostgreSQL 初始化脚本
-- 在本地数据库执行：psql "$DATABASE_URL" -f migrations/20260806_092805_create_servers_and_metrics_tables.sql
-- 本项目不使用默认 public schema，所有对象创建在 pro_monitor 下
-- ============================================

-- 创建独立 schema（隔离本项目数据，不污染 public）
CREATE SCHEMA IF NOT EXISTS pro_monitor;

-- 在 pro_monitor 下创建 servers 表
CREATE TABLE IF NOT EXISTS pro_monitor.servers (
  id SERIAL PRIMARY KEY,
  name VARCHAR(255) NOT NULL,
  provider VARCHAR(100),
  billing_cycle VARCHAR(50),
  price DECIMAL(10, 2) DEFAULT 0,
  shared_secret VARCHAR(255) NOT NULL UNIQUE,
  status VARCHAR(20) DEFAULT 'offline',
  last_seen TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- 在 pro_monitor 下创建 metrics 表
CREATE TABLE IF NOT EXISTS pro_monitor.metrics (
  id SERIAL PRIMARY KEY,
  server_id INTEGER NOT NULL REFERENCES pro_monitor.servers(id) ON DELETE CASCADE,
  timestamp TIMESTAMPTZ NOT NULL,
  cpu_cores INTEGER DEFAULT 0,
  cpu_usage REAL DEFAULT 0,
  memory_total BIGINT DEFAULT 0,
  memory_usage REAL DEFAULT 0,
  disk_total BIGINT DEFAULT 0,
  disk_used_percent REAL DEFAULT 0,
  network_in REAL DEFAULT 0,
  network_out REAL DEFAULT 0,
  ping_beijing_telecom REAL DEFAULT 0,
  ping_beijing_unicom REAL DEFAULT 0,
  ping_beijing_mobile REAL DEFAULT 0,
  ping_shanghai_telecom REAL DEFAULT 0,
  ping_shanghai_unicom REAL DEFAULT 0,
  ping_shanghai_mobile REAL DEFAULT 0,
  ping_guangzhou_telecom REAL DEFAULT 0,
  ping_guangzhou_unicom REAL DEFAULT 0,
  ping_guangzhou_mobile REAL DEFAULT 0,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 创建索引（同样位于 pro_monitor 下）
CREATE INDEX IF NOT EXISTS idx_metrics_server_timestamp ON pro_monitor.metrics(server_id, timestamp);
