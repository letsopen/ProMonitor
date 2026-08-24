# ProMonitor 设计文档（目标架构 v3 · 自托管）

> 状态：demo → 成熟个人服务器探针服务。**本版本不再依赖任何云平台**：无 Meoo 线上部署、无云函数、无云数据库，纯自托管单机。本文档是后续实现的契约。

## 1. 术语
- **被控**：提供监控数据的服务器实例（运行 Agent）。
- **主控**：本监控面板服务（Go 后端二进制 + Vue 前端）。

## 2. 为什么改成自托管 + SQLite
上一版设计依赖 Meoo 镜像 + Supabase 云数据库。你决定不再上云，因此：
- **部署形态变了**：从"无状态云函数 + 托管数据库"变为"你自己的服务器/VPS 上常驻一个二进制进程"。这反而**简化了**很多——不再是冷启动/无状态约束，内存聚合、长连接推送都更自然。
- **数据库选型重做**：云数据库不在选项内。个人单节点、数据量极小（见 §4 量级）、对资源敏感 → **SQLite 是最优解**：单文件、零运维、零额外进程、纯 Go 驱动仍保持单二进制、WAL 模式下并发够用。

| 候选 | 结论 |
|---|---|
| **SQLite（选定）** | 单文件、零运维、资源占用最低、modernc 纯 Go 驱动无 cgo，仍是单二进制 |
| 自托管 PostgreSQL | 杀鸡用牛刀，多一个常驻进程与运维负担，不值 |
| BoltDB / 嵌入式 KV | 无 SQL、聚合/历史查询要自己写，不如 SQLite 直接 |
| 云数据库（Supabase 等） | 已排除 |

## 3. 技术栈决策
| 层 | 选择 | 理由 |
|---|---|---|
| 主控后端 | **Go 1.23+**，单一静态二进制（CGO_ENABLED=0）监听 `:9000` | 极致性能、极低内存、goroutine 天然适配高频上报、单文件部署 |
| 被控 Agent | **Go**，单二进制 | 跨平台、零运行时依赖、资源占用极低 |
| 前端 | Vue3 + Vite + Element Plus（沿用） | 已半成熟，Go 后端直接托管 `dist/` |
| 数据库 | **SQLite**（单文件 `promonitor.db`，WAL） | 零运维、零额外进程、资源敏感友好 |
| 缓存 | **纯进程内内存**（无 Redis） | 单实例、资源敏感，避免额外服务 |
| 部署 | 自托管：二进制 + systemd 守护 + 可选 nginx/caddy 反代 HTTPS | 你自己的机器，常驻进程，无需云 |

## 4. 数据采集与聚合（核心，不变）
- 被控 Agent 每 **30s** 采集一次，POST 到 `主控 /api/ingest`，Body 经 **HMAC-SHA256** 签名（预共享 `HMAC_SECRET`），请求头 `X-Signature`。
- 主控**不持久化原始高频数据**：`/api/ingest` 仅做验签 + 写入内存聚合缓冲。
- 每个被控一个 **10 分钟窗口**缓冲（累加器）：累计 CPU/内存/硬盘/网络均值所需的和与计数；50 个 ping 节点各自累计「有效和 + 有效计数」，**>1000ms 视为无效不计入**。
- 全局定时器每 30s 扫描，窗口满 10 分钟即结算：生成 1 行 `metrics_agg`，写入 SQLite，并刷新 `latest_snapshot`；随后重置该窗口。
- 被控元数据（`servers` 表）在结算时 upsert（每 10 分钟一次，避免高频写库）。

**数据量级**（证明 SQLite 完全够用）：30 天 × 10 分钟 = 每被控 4320 行；即便 50 台被控也仅 ~21.6 万行，对 SQLite 零压力，全部走 `(server_id, ts)` 索引。

## 4.1 网络延迟节点：主控统一管理（v4 新增）
- **节点清单由主控统一配置**，不再是各被控本地硬编码。主控新增**公开匿名端点** `GET /api/ping-config`，返回：
  ```json
  { "type": "icmp" | "tcp", "nodes": [{"id":1,"name":"...","ip":"1.1.1.1","port":80}, ...] }
  ```
- 探测方式来自环境变量 `PING_TYPE`（`tcp` 或 `icmp`，默认 `tcp`）。
- 节点清单存 SQLite `ping_nodes(id, name, ip, port, created_at)` 表，由管理后台维护：
- ⚠️ **ICMP 容器限制**：Alpine 容器内默认无 `CAP_NET_RAW`，raw socket 建不了，ICMP 探测会失败。要让 `PING_TYPE=icmp` 生效，主控/Agent 容器启动时需加 `--cap-add NET_RAW`：
  ```bash
  # 主控
  docker run -d --name promonitor --restart unless-stopped -p 9000:9000 --cap-add NET_RAW \
    -e HMAC_SECRET='...' -e ADMIN_PASS='...' -e SESSION_SECRET='...' \
    -e PING_TYPE=icmp -v promonitor-data:/app/data promonitor:latest
  # 被控 Agent（探测在 Agent 侧执行，同样需要该能力）
  docker run -d --name promonitor-agent --restart unless-stopped --network host --cap-add NET_RAW \
    -e MASTER_URL='http://<主控IP>:9000' -e HMAC_SECRET='...' -e SERVER_ID='web-01' promonitor:latest promonitor-agent
  ```
  使用 docker compose 时在服务下加 `cap_add: [NET_RAW]`。**默认推荐 TCP**，兼容性最好（无需任何额外能力）。

## 4.2 数据时间以被控测量时间为准（v4 新增）
- 被控上报的 `ts` 字段为**测量时间（unix 秒）**，主控以此为数据时间，**不采用服务端接收时间**。
- 聚合改用「按测量时间对齐到 10 分钟桶」：`桶起点 = floor(ts / 600) × 600`。结算时落库 `ts` = 桶起点。
- **收益**：即便被控因网络故障积压数小时数据，落库时间仍是真实测量时间，曲线不会"挤"在恢复那一刻，历史不被污染。
- 仅当样本 `ts<=0`（异常）时才回退到服务端接收时间，避免强退。

## 4.3 被控积压暂存 + 自动重试（v4 新增）
- 被控 Agent 改为**带内存队列的发送器**：采集结果先入队，后台 goroutine 顺序出队上传。
- 上传失败（网络错误 / 非 2xx）按**指数退避**重试（base 2s → 上限 60s，最多 5 次），成功才出队。达到上限后丢弃该样本（避免永久阻塞队列）。
- **队列上限保护**（默认 500 条，超限丢最旧）防止 OOM。
- 说明：积压队列为**纯内存**，进程重启会丢失未发数据——对监控探针是合理权衡（重启后重新开始采样）；如需持久化可后续加磁盘队列（WAL/bolt）。


- `servers(id, name, ip, secret, status, created_at)` — 被控元数据。`created_at` 用 unix 秒整数。
- `ping_nodes(id PK, name, ip, port, created_at)` — 探测节点清单，由管理后台维护。
- `metrics_agg(server_id, ts, cpu_avg, mem_avg, disk_avg, net_in_avg, net_out_avg, ping_nodes TEXT)` — 聚合结果。**ts 为 unix 秒整数**（排序/保留期清理都靠它）。50 节点压成 1 行 JSON数组。
- `latest_snapshot(server_id PK, cpu, mem, disk, net_in, net_out, pings, updated_at)` — 列表/详情实时读。`server_id` 外键 `ON DELETE CASCADE`。
- `admin_users(username PK, password_hash, created_at)` — 单 admin。
- **保留期可配置**：不做分区（SQLite 无原生分区），由应用层 `PruneOld(RETENTION_DAYS)`（默认 7 天）执行 `DELETE FROM metrics_agg WHERE ts < now-retention`，随后 `VACUUM` 回收空间。启动时与每日定时各跑一次。数据量极小，`DELETE` + `VACUUM` 远快于 PostgreSQL 分区 `DROP`，且更简单。
- 索引：`metrics_agg(server_id, ts DESC)`；`latest_snapshot(server_id)`。
- **WAL 模式 + busy_timeout + 串行连接**：SQLite 单写者，连接数设为 1 串行化，彻底避免 `database is locked`；写入极低频（每被控每 10 分钟 1 行）+ 轮询读取，1 连接绰绰有余。

## 6. 实时展示
- 数据源：`latest_snapshot`（重启后也能立刻展示上次落库的最新值，因为列表/详情都直接读 SQLite，不依赖内存）。
- 列表页 / 详情页轮询 `GET /api/servers/latest`，间隔 **5–10s**。
- 详情页进入时拉一次保留期内 `metrics_agg` 渲染历史曲线（前端按区间降采样：近 24h 用全量 144 点，更长区间按小时均值再聚合），之后轮询追加最新点。
- 详情页后续可升级为 SSE 单向推送（自托管常驻进程无云函数冷启动顾虑，SSE 很合适）；**不采用 WebSocket**。

## 7. 鉴权（匿名列表/详情 + 管理页登录）
- 单一管理员 `admin`，无注册、无多用户。`admin_users` 单行种子（首次启动用 `ADMIN_PASS` 哈希写入）。
- 密码 `bcrypt` 哈希，绝不明文；传输走 HTTPS（自托管时由前面 nginx/caddy 反代提供 TLS）。
- 会话：`httpOnly` + `SameSite=Lax` Cookie，存**签名 session**（用户名+过期，HMAC 签名），**不放前端 JS 可读存储**（防 XSS）。
- 路由边界：
  - 匿名：`GET /api/servers`、`GET /api/servers/:id/metrics`、`GET /api/servers/latest`。
  - 鉴权：`POST /api/admin/login|logout|change-password`，以及所有 `POST/PUT/DELETE /api/admin/servers/*`。
  - 前端 Vue Router `beforeEach` 仅做体验层隐藏，**真拦截在服务端 `requireAdmin` 中间件**。
- 操作鉴权：单 admin 即 `req.session.isAdmin`；危险操作（删全部数据）加二次密码确认。
- 加固：登录失败限流、被控上报 HMAC 验签、Cookie 防 CSRF（SameSite + 必要时 double-submit）。
- 改密：`/api/admin/change-password` 校验旧密码 → 更新哈希；首次部署强制改密。

## 8. API 一览
| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/ingest` | HMAC | 被控上报（`ts`=测量时间 unix 秒） |
| GET | `/api/servers` | 匿名 | 列表（含最新快照） |
| GET | `/api/servers/latest` | 匿名 | 轮询最新占用 |
| GET | `/api/servers/:id/metrics?from&to` | 匿名 | 历史聚合 |
| GET | `/api/ping-config` | 匿名 | 被控拉取探测配置（type/nodes，nodes 来自 ping_nodes 表） |
| POST | `/api/admin/login` | 公开 | 登录 |
| POST | `/api/admin/logout` | 登录 | 登出 |
| POST | `/api/admin/change-password` | admin | 改密 |
| POST | `/api/admin/servers` | admin | 新增被控 |
| PUT | `/api/admin/servers/:id` | admin | 编辑 |
| DELETE | `/api/admin/servers/:id` | admin | 删除 |

| POST | `/api/admin/ping-nodes` | admin | 新增探测节点 |
| PUT | `/api/admin/ping-nodes/{id}` | admin | 编辑探测节点 |
| DELETE | `/api/admin/ping-nodes/{id}` | admin | 删除探测节点 |

## 9. 部署（自托管）
- `scripts/setup.sh`：装 Go → `cd server && go mod tidy && CGO_ENABLED=0 go build -o ../bin/server` → 前端 `npm i && npm run build`。
- `scripts/start.sh`：设置环境变量后 `exec ./bin/server`（监听 `:9000`，托管 `dist/`）。
- **守护进程**（任选）：systemd unit 或 supervisor，保证崩溃自启。
- **HTTPS**：在前面套 `nginx` / `caddy` 反代 `localhost:9000`，由反代终结 TLS（caddy 自动证书最省心）。
- **环境变量**：`DB_PATH`(默认 `./promonitor.db`) / `HMAC_SECRET`(必填) / `ADMIN_USER`(默认 admin) / `ADMIN_PASS`(首次种子) / `SESSION_SECRET` / `PORT`(默认 9000) / `FRONTEND_DIR`(默认 `./dist`) / `PING_TYPE`(默认 tcp)。节点清单通过管理后台存 `ping_nodes` 表。。
- 被控 Agent 部署：把 `agent` 编译出的二进制放到各被控机，以 `HMAC_SECRET` 与主控地址启动即可；延迟节点与方法由主控统一下发，无需在被控侧配置。

## 10. 目录结构
```
server/      Go 主控后端（cmd/server + internal/{config,store,ingest,aggregator,auth,api,metrics}）
agent/       Go 被控 Agent
migrations/  SQLite 建表 SQL
scripts/     setup.sh(构建) / start.sh(启动) —— 自托管
src/         Vue 前端（沿用，调用上述 API）
```

## 11. 迁移自 v2 的要点
- 数据库：PostgreSQL/Supabase → **SQLite**（store 层重写，接口不变）。
- 时间戳统一改用 **unix 秒整数**存储（排序与保留期清理更简单）。
- 30 天保留：分区 `DROP` → 应用层 `DELETE + VACUUM`。
- 部署：Meoo 镜像 → 自托管二进制 + systemd + 反代。
- 其余（HMAC 上报、10 分钟聚合、单 admin 鉴权、API、前端）保持不变。
