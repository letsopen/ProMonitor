# 项目技术上下文

## 技术栈（v4 自托管 · Docker / Alpine 部署）
- **主控后端**: Go（CGO_ENABLED=0 静态二进制），监听 `:9000`，同时托管前端 `dist/`
- **被控 Agent**: Go 单二进制，零运行时依赖，按 `COLLECT_INTERVAL`（默认 30s）上报实时数据
- **前端**: Vue3 + Vite + Element Plus（单页应用）
- **数据库**: SQLite（单文件 `promonitor.db`，WAL 模式，纯 Go 驱动 modernc，无 cgo）
- **部署**: Docker，运行期基础镜像 **Alpine 3.21**；镜像内**零编译**——Go 二进制由 CI 交叉编译、前端 dist 由 CI 构建，Dockerfile 仅做组装 COPY
- **源码构建下限**: Go ≥ 1.18（modernc.org/sqlite 要求 1.20、gopsutil/v4 要求 1.18；本地验证用 Go 1.23）。
  目标机无需装 Go——Docker 镜像复用 CI 产出的 `bin/` 静态二进制，或直接使用 `bin/` 预编译静态二进制。
- **交叉编译（linux/amd64、linux/arm64）由 GitHub Actions 完成**：见 `.github/workflows/build-binaries.yml`。
  push 到 `main` 产出 workflow artifacts；推送 `v*` 标签自动发布 GitHub Release（含 `promonitor-server_*` / `promonitor-agent_*`）。
  **本地不再需要手工编译 linux 二进制包**，日常开发只需在宿主平台 `go build ./...` 做编译校验即可。

## 目录结构
```
server/      Go 主控后端
  cmd/server/main.go        入口：聚合器+API+静态托管+30天保留清理
  internal/config           环境变量
  internal/store            SQLite 存储层（modernc 纯 Go 驱动，内联 schema，无 cgo、无 embed）
  internal/ingest           HMAC 验签 + 写入聚合缓冲
  internal/aggregator      按测量时间对齐的 10 分钟桶均值聚合（首次上报即 UpsertServer 注册）
  internal/auth            单 admin 登录/会话/中间件
  internal/api             HTTP 路由与处理器
  internal/metrics         共享类型
agent/       Go 被控 Agent（gopsutil 采集 + HMAC 上报）
migrations/  SQLite 建表 SQL 参考（实际由 server 启动时自动执行内联 schema）
scripts/     smoke.mjs（端到端冒烟测试）
src/         Vue 前端
bin/         预编译静态二进制（linux amd64/arm64，由 CI 生成，不提交到仓库，供非 Docker 部署直接取用）
Dockerfile / docker-compose.yml / .dockerignore   容器化部署（镜像由 CI 构建推送 GHCR，本地不编译）
```

## 架构与数据流
```
被控Agent(Go)
        │  每 COLLECT_INTERVAL 秒采集
        │  POST /api/ingest -> 主控刷新 latest_snapshot（实时，不落历史）
        │  本地 10 分钟窗口聚合 -> POST /api/history -> 主控落库 metrics_agg
        │  GET /api/ping-config(每5分钟) -> 探测配置
        
主控 Server
        ├─ /api/ingest  刷新 latest_snapshot
        ├─ /api/history 写入 metrics_agg
        └─ REST API + 托管 dist/
            │
            
SQLite (promonitor.db) + Vue 前端轮询

Vue前端 <--GET /api/servers(轮询10s)-- 读取最新快照(latest_snapshot JOIN)
Vue前端 <--GET /api/servers/:id/metrics-- 读取30天聚合历史
```

## 关键设计决策
- **实时/历史双通道分离**：
  - 实时：`/api/ingest` 仅刷新 `latest_snapshot`，不落历史库，失败不重试。
  - 历史：被控本地做 10 分钟窗口聚合，在整数 10 分钟边界（0/10/20/... 分）通过 `/api/history` 批量上报；失败本地持久化，自动重试。
- **数据时间 = 被控测量时间**：样本 `ts` 为测量时间（unix 秒），历史聚合按 `floor(ts/600)*600` 对齐到 10 分钟桶，落库 `ts` 即桶起点。网络故障积压的数据补传后落在真实时间点，不污染恢复时刻的曲线。
- **ping 节点主控统一下发**：`GET /api/ping-config`（匿名）返回 `{type, nodes}`；节点清单存在 SQLite `ping_nodes` 表，由管理后台 `GET/POST/PUT/DELETE /api/admin/ping-nodes` 维护；被控启动拉取 + 每 5 分钟热更新；`-targets` 仅作主控未配置节点时的回退。
- **被控历史数据可靠性**：Agent 本地 SQLite 持久化待上传记录，后台按批量（50 条/次）+ 指数退避重试，成功后删除；恢复后自动续传。
- **缓存 = 纯进程内内存**，无 Redis（资源敏感、单实例）。
- **首次上报即注册**：`/api/ingest` 处理时异步 `UpsertServer`，被控无需先在管理页添加。
- **30 天保留**：SQLite 不做分区，应用层每日 `PruneOld(30)` 删除过期行并 `VACUUM`。
- **ping 无效判定**：延迟 >1000ms 不计入均值（哨兵值 -1）。
- **鉴权**：单 admin，密码 bcrypt + httpOnly Cookie 签名会话；列表/详情匿名，管理页 `RequireAdmin` 中间件服务端拦截。
- **SQLite 连接策略**：WAL + busy_timeout + 单连接串行，避免 `database is locked`。
- **前端托管**：主控直接用 `http.FileServer` + NotFound 回退托管 `dist/`，支持 Vue History 路由；无需 nginx（公网 HTTPS 时再前置 nginx）。

## 安全
- 被控上报：HMAC-SHA256 签名验签（预共享 `HMAC_SECRET`），请求头 `X-Signature`。
- 管理接口：登录限流 + Cookie 防 CSRF（SameSite）+ 密码 bcrypt 哈希。
- 传输安全：自托管/容器内由前置 nginx 反代终结 TLS（主控本身不内置 HTTPS）。

## 环境变量（主控）
`DB_PATH`(必填，容器默认 /app/data/promonitor.db) / `HMAC_SECRET`(必填) / `ADMIN_USER`(默认 admin) / `ADMIN_PASS`(首次种子) / `SESSION_SECRET` / `PORT`(默认 9000) / `FRONTEND_DIR`(默认 ./dist) / `PING_TYPE`(默认 tcp)

ping 节点清单不再通过环境变量维护，而是通过管理后台“延迟节点”页面存 SQLite `ping_nodes` 表。

## 部署（Docker，推荐）

> 镜像由 GitHub Actions 构建并推送 GHCR（`ghcr.io/letsopen/promonitor:latest`，见 `.github/workflows/docker-image.yml`），
> 本地/服务器只需 pull，无需安装 Go/Node，也不执行 docker build。

### docker run（推荐，主控）
```
# TCP 探测（默认）
docker run -d --name promonitor --restart unless-stopped -p 9000:9000 \
  -e HMAC_SECRET='<随机长串>' -e ADMIN_PASS='<初始密码>' -e SESSION_SECRET='<随机串>' \
  -e PING_TYPE=tcp -v promonitor-data:/app/data ghcr.io/letsopen/promonitor:latest

# ICMP 探测：必须加 --cap-add NET_RAW（Alpine 默认无 CAP_NET_RAW，不加则探测全失败）
docker run -d --name promonitor --restart unless-stopped -p 9000:9000 --cap-add NET_RAW \
  -e HMAC_SECRET='<随机长串>' -e ADMIN_PASS='<初始密码>' -e SESSION_SECRET='<随机串>' \
  -e PING_TYPE=icmp -v promonitor-data:/app/data ghcr.io/letsopen/promonitor:latest
# 访问 http://<host>:9000 ；数据持久化在 promonitor-data 卷
# 进入管理后台 → 延迟节点 中添加探测节点（id/name/ip/port），被控会自动拉取
```

### docker run（被控 Agent）
```
# TCP 探测（跟随主控配置）
docker run -d --name promonitor-agent --restart unless-stopped --network host \
  -e MASTER_URL='http://<主控IP>:9000' -e HMAC_SECRET='<与主控一致>' \
  -e SERVER_ID='web-01' -e SERVER_NAME='阿里云-杭州' -e SERVER_IP='10.0.0.5' \
  ghcr.io/letsopen/promonitor:latest promonitor-agent

# ICMP 探测：同样需要 --cap-add NET_RAW
docker run -d --name promonitor-agent --restart unless-stopped --network host --cap-add NET_RAW \
  -e MASTER_URL='http://<主控IP>:9000' -e HMAC_SECRET='<与主控一致>' \
  -e SERVER_ID='web-01' -e SERVER_NAME='阿里云-杭州' -e SERVER_IP='10.0.0.5' \
  ghcr.io/letsopen/promonitor:latest promonitor-agent
```

### docker compose（备选）
```
cp .env.example .env      # 填 HMAC_SECRET、ADMIN_PASS，可选填 PING_TYPE
docker compose up -d      # 拉取 ghcr.io 镜像并启动（无需本地构建）
# 访问 http://<host>:9000 ；数据持久化在 promonitor-data 卷
# 使用 ICMP 时需在 docker-compose.yml 的 promonitor 服务加 cap_add: [NET_RAW]
# 进入管理后台 → 延迟节点 中添加探测节点
```

## 被控 Agent 启动参数
`-master MASTER_URL` / `-secret HMAC_SECRET`(必填) / `-id SERVER_ID`(必填) / `-name` / `-ip` / `-targets`(可选覆盖) / `-interval`(默认30s)
对应环境变量：
- `MASTER_URL` / `HMAC_SECRET` / `SERVER_ID` / `SERVER_NAME` / `SERVER_IP`
- `PING_TARGETS`：可选回退节点
- `COLLECT_INTERVAL`：实时采集间隔（秒），默认 30，最低 30，必须是 30 的倍数

延迟节点与方法由主控统一下发（Agent 拉取 `/api/ping-config`，每 5 分钟热更新）；`-targets` 仅当主控未配置节点时回退使用。
Agent 本地做 10 分钟窗口聚合，并通过 `/api/history` 上报；失败本地持久化 + 自动重试。
