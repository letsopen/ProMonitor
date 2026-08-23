-- ProMonitor 初始化 schema（由 server 启动时自动执行，无需手动运行）
-- SQLite 单文件 + WAL；30 天保留由应用层 PruneOld() 执行（DELETE + VACUUM）。

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS servers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    ip          TEXT,
    secret      TEXT,                       -- 预留：每被控独立密钥（当前全局 HMAC_SECRET）
    status      INTEGER NOT NULL DEFAULT 1,-- 1=在线 0=离线
    created_at  INTEGER NOT NULL            -- unix 秒
);

CREATE TABLE IF NOT EXISTS metrics_agg (
    server_id   TEXT NOT NULL,
    ts          INTEGER NOT NULL,
    cpu_avg     REAL,
    mem_avg     REAL,
    disk_avg    REAL,
    net_in_avg  REAL,
    net_out_avg REAL,
    ping_nodes  TEXT,                        -- JSON: [50] 各节点均值(ms)，无效为 -1
    PRIMARY KEY (server_id, ts)
);

CREATE INDEX IF NOT EXISTS idx_metrics_agg_lookup
    ON metrics_agg (server_id, ts DESC);

CREATE TABLE IF NOT EXISTS latest_snapshot (
    server_id   TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    cpu         REAL,
    mem         REAL,
    disk        REAL,
    net_in      REAL,
    net_out     REAL,
    pings       TEXT,
    updated_at  INTEGER NOT NULL             -- unix 秒
);

CREATE TABLE IF NOT EXISTS admin_users (
    username      TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    created_at    INTEGER NOT NULL            -- unix 秒
);
