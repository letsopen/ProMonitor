-- ProMonitor 初始化迁移（SQLite，单文件 + WAL）
-- 说明：服务端启动时已通过 server/internal/store/store.go 内嵌 schemaDDL 自动执行等价 DDL，
--       本文件仅作为人工审阅/手动初始化的参考，内容与内嵌 schema 保持一致。
-- 保留期（RETENTION_DAYS，默认 7 天）由应用层 PruneOld() 执行（DELETE + VACUUM），不依赖分区。

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS servers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    ip          TEXT,
    secret      TEXT,
    status      INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS metrics_agg (
    server_id   TEXT NOT NULL,
    ts          INTEGER NOT NULL,
    cpu_avg     REAL,
    mem_avg     REAL,
    disk_avg    REAL,
    net_in_avg  REAL,
    net_out_avg REAL,
    ping_nodes  TEXT,
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
    cpu_cores   INTEGER,
    mem_total   INTEGER,
    disk_total  INTEGER,
    pings       TEXT,
    updated_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_latest_snapshot_updated
    ON latest_snapshot (updated_at DESC);

CREATE TABLE IF NOT EXISTS admin_users (
    username      TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS ping_nodes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    ip          TEXT NOT NULL,                -- 仅 IPv4
    port        INTEGER NOT NULL DEFAULT 80,  -- TCP 探测端口
    created_at  INTEGER NOT NULL
);
