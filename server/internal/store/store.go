package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"promonitor/server/internal/metrics"
)

// schemaDDL 是初始化建表语句，内联以避免 //go:embed（Go 1.16+ 才支持），
// 同时也让单二进制发布时无需附带外部 .sql 文件。
const schemaDDL = `
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
    pings       TEXT,
    updated_at  INTEGER NOT NULL
);

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
`

// Store 封装 SQLite 访问（database/sql + 纯 Go 驱动，无 cgo，仍是单二进制）
type Store struct {
	db *sql.DB
}

// New 打开（必要时创建）SQLite 数据库文件。
// dbPath 为空时默认 ./promonitor.db。WAL + busy_timeout + 外键约束在 DSN 里启用。
func New(ctx context.Context, dbPath string) (*Store, error) {
	if dbPath == "" {
		dbPath = "promonitor.db"
	}
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)",
		dbPath,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite 单写者模型：串行化连接可彻底避免 "database is locked"，
	// 本服务写入极低（每被控每 10 分钟 1 行）+ 轮询读取，1 连接足够。
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	// 首次启动自动建表（幂等，生产也可手动跑 migrations/001_init.sql）
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// --- 管理员 ---

func (s *Store) EnsureAdmin(ctx context.Context, username, hash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_users(username, password_hash, created_at) VALUES(?,?,?)
		 ON CONFLICT(username) DO NOTHING`, username, hash, time.Now().Unix())
	return err
}

func (s *Store) GetAdminHash(ctx context.Context, username string) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM admin_users WHERE username=?`, username).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("admin not found")
	}
	if err != nil {
		return "", err
	}
	return h, nil
}

func (s *Store) SetAdminPassword(ctx context.Context, username, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_users SET password_hash=? WHERE username=?`, hash, username)
	return err
}

// --- 被控元数据 ---

func (s *Store) UpsertServer(ctx context.Context, id, name, ip string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO servers(id, name, ip, status, created_at) VALUES(?,?,?,1,?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, ip=excluded.ip, status=1`,
		id, name, ip, time.Now().Unix())
	return err
}

func (s *Store) CreateServer(ctx context.Context, id, name, ip string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO servers(id, name, ip, status, created_at) VALUES(?,?,?,1,?)
		 ON CONFLICT(id) DO NOTHING`, id, name, ip, time.Now().Unix())
	return err
}

func (s *Store) UpdateServer(ctx context.Context, id, name, ip string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET name=?, ip=? WHERE id=?`, name, ip, id)
	return err
}

func (s *Store) DeleteServer(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, id)
	return err
}

// --- 聚合指标 ---

// InsertAgg 写入一条 10 分钟聚合行（ts 以 unix 秒整数存储，便于排序与保留期清理）
func (s *Store) InsertAgg(ctx context.Context, a metrics.AggRow) error {
	pings, _ := json.Marshal(a.Pings)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO metrics_agg(server_id, ts, cpu_avg, mem_avg, disk_avg, net_in_avg, net_out_avg, ping_nodes)
		 VALUES(?,?,?,?,?,?,?,?)`,
		a.ServerID, a.TS, a.CPU, a.Mem, a.Disk, a.NetIn, a.NetOut, string(pings))
	return err
}

func (s *Store) UpdateSnapshot(ctx context.Context, a metrics.AggRow) error {
	pings, _ := json.Marshal(a.Pings)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO latest_snapshot(server_id, cpu, mem, disk, net_in, net_out, pings, updated_at)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON CONFLICT(server_id) DO UPDATE SET
		   cpu=excluded.cpu, mem=excluded.mem, disk=excluded.disk,
		   net_in=excluded.net_in, net_out=excluded.net_out, pings=excluded.pings,
		   updated_at=excluded.updated_at`,
		a.ServerID, a.CPU, a.Mem, a.Disk, a.NetIn, a.NetOut, string(pings), time.Now().Unix())
	return err
}

func (s *Store) ListServers(ctx context.Context) ([]metrics.ServerView, error) {
	q := `SELECT s.id, s.name, s.ip, s.status,
				 sn.cpu, sn.mem, sn.disk, sn.net_in, sn.net_out, sn.pings, sn.updated_at
		  FROM servers s
		  LEFT JOIN latest_snapshot sn ON sn.server_id = s.id
		  ORDER BY s.created_at DESC`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]metrics.ServerView, 0)
	for rows.Next() {
		var v metrics.ServerView
		var pings []byte
		var updatedAt sql.NullInt64
		var cpu, mem, disk, netIn, netOut sql.NullFloat64
		if err := rows.Scan(&v.ID, &v.Name, &v.IP, &v.Status,
			&cpu, &mem, &disk, &netIn, &netOut, &pings, &updatedAt); err != nil {
			return nil, err
		}
		if cpu.Valid {
			v.CPU = &cpu.Float64
		}
		if mem.Valid {
			v.Mem = &mem.Float64
		}
		if disk.Valid {
			v.Disk = &disk.Float64
		}
		if netIn.Valid {
			v.NetIn = &netIn.Float64
		}
		if netOut.Valid {
			v.NetOut = &netOut.Float64
		}
		if len(pings) > 0 {
			_ = json.Unmarshal(pings, &v.Pings)
		}
		if updatedAt.Valid {
			v.UpdatedAt = time.Unix(updatedAt.Int64, 0)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetHistory(ctx context.Context, serverID string, from, to time.Time, limit int) ([]metrics.AggRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT server_id, ts, cpu_avg, mem_avg, disk_avg, net_in_avg, net_out_avg, ping_nodes
		 FROM metrics_agg WHERE server_id=? AND ts>=? AND ts<=? ORDER BY ts ASC`,
		serverID, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]metrics.AggRow, 0)
	for rows.Next() {
		var a metrics.AggRow
		var pings []byte
		var ts int64
		if err := rows.Scan(&a.ServerID, &ts, &a.CPU, &a.Mem, &a.Disk, &a.NetIn, &a.NetOut, &pings); err != nil {
			return nil, err
		}
		a.TS = ts
		if len(pings) > 0 {
			_ = json.Unmarshal(pings, &a.Pings)
		}
		out = append(out, a)
	}
	if limit > 0 && len(out) > limit {
		step := len(out) / limit
		if step < 1 {
			step = 1
		}
		var sampled []metrics.AggRow
		for i := 0; i < len(out); i += step {
			sampled = append(sampled, out[i])
		}
		out = sampled
	}
	return out, rows.Err()
}

// --- 保留期 ---

// PruneOld 删除 retentionDays 天前的聚合行。SQLite 不做分区，直接按 ts 删除。
// 返回被删除行数。由 main 在启动时与每日定时调用。
func (s *Store) PruneOld(ctx context.Context, retentionDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
	res, err := s.db.ExecContext(ctx, `DELETE FROM metrics_agg WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Vacuum 回收被删除行占用的空间（保留期清理后调用，避免文件膨胀）
func (s *Store) Vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `VACUUM`)
	return err
}

// --- Ping 节点管理（主控统一维护，被控经 /api/ping-config 拉取） ---

func (s *Store) ListPingNodes(ctx context.Context) ([]metrics.PingNode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, ip, port FROM ping_nodes ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]metrics.PingNode, 0)
	for rows.Next() {
		var n metrics.PingNode
		if err := rows.Scan(&n.ID, &n.Name, &n.IP, &n.Port); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) CreatePingNode(ctx context.Context, name, ip string, port int) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ping_nodes(name, ip, port, created_at) VALUES(?,?,?,?)`,
		name, ip, port, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdatePingNode(ctx context.Context, id int64, name, ip string, port int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ping_nodes SET name=?, ip=?, port=? WHERE id=?`, name, ip, port, id)
	return err
}

func (s *Store) DeletePingNode(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ping_nodes WHERE id=?`, id)
	return err
}
