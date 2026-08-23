# ProMonitor — 个人服务器探针服务

一个**自托管**的服务器性能监控面板：被控（被监控的服务器）上运行轻量 Agent 周期性采集
CPU / 内存 / 硬盘 / 网络 / 多节点 Ping 延迟并上报；主控聚合后落库，前端展示列表与详情。

- 主控后端 / 被控 Agent 均为 **Go 单文件静态二进制**（零依赖）。
- 存储用 **SQLite 单文件**（纯 Go 驱动，无 cgo）。
- **实时数据**只刷新 latest_snapshot 供前端展示，**不落历史库**，失败不重试。
- **历史趋势数据**由被控本地做 10 分钟窗口聚合后，通过 `/api/history` 上报主控落库；失败本地持久化 + 自动重试。
- 前端 **Vue3 + Vite + Element Plus**，由主控直接托管 `dist/`（无需额外 nginx，公网部署时再在前面套 nginx 配证书）。
- 部署方式：**Docker（Alpine 3.21 基础镜像）**，构建一次、随处运行。

---

## 架构

```
被控 Agent (Go)
   │  每 COLLECT_INTERVAL 秒采集一次
   │  POST /api/ingest（实时快照，HMAC 验签，失败不重试）
   │  本地 10 分钟窗口聚合 → POST /api/history（失败本地持久化 + 自动重试）
   ▼
主控 Server (Go :9000)
   ├─ /api/ingest 刷新 latest_snapshot（列表/详情读它，极轻，不落历史）
   ├─ /api/history 直接落库 metrics_agg（保留 30 天）
   └─ 托管前端 dist/  +  REST API
            │
            ▼
   SQLite (promonitor.db)  +  前端轮询（每 10s）
```

鉴权边界：
- 匿名可读：服务器列表、服务器详情历史。
- 必须登录（单管理员 `admin`）：管理页增删改被控、修改密码。服务端 `RequireAdmin` 中间件做真拦截，前端路由守卫仅作体验层。

---

## 目录结构

```
server/   主控后端（Go）：config / store(sqlite) / ingest(hmac) / aggregator / auth / api
agent/    被控 Agent（Go）：gopsutil 采集 + HMAC 上报
src/      前端（Vue3 + Vite + Element Plus）
migrations/  SQL 参考（实际建表由 server 启动时自动执行 schema）
scripts/   构建/冒烟脚本（smoke.mjs）
Dockerfile / docker-compose.yml / .dockerignore   容器化部署
bin/       预编译静态二进制（linux amd64/arm64，供非 Docker 部署直接取用）
```

---

## 快速开始（Docker 部署）

前置：已安装 Docker。三种方式任选其一：**从 GitHub 拉镜像（免构建）**、**`docker run`（推荐，单命令）** 或 **docker compose**。

### 方式 0：直接从 GitHub 拉取镜像（无需本地构建）

项目已配置 GitHub Actions 流水线：push 到 `main` 自动构建并推送镜像到
**GitHub Container Registry (GHCR)**（`linux/amd64`）。

```bash
# 拉取最新镜像（首次）或更新
docker pull ghcr.io/letsopen/promonitor:latest
```

> 私有仓库的镜像默认**私有**：如需公开拉取，到 GitHub →
> 你的头像 → Your packages → `promonitor` → Package settings → Change visibility → Public。
> 打 `v*` 标签（`git tag v1.0.0 && git push origin v1.0.0`）会额外构建并推送
> `1.0.0` / `1.0` / `1` 版本镜像。

### 方式 A：docker run（推荐）

```bash
# 1) 先构建镜像（首次或代码更新后）
docker build -t promonitor:latest .

# 2) 启动主控（TCP 延迟探测，默认）
docker run -d --name promonitor --restart unless-stopped \
  -p 9000:9000 \
  -e HMAC_SECRET='<与所有被控一致的随机长串>' \
  -e ADMIN_PASS='<初始管理员密码>' \
  -e SESSION_SECRET='<随机串>' \
  -e PING_TYPE=tcp \
  -v promonitor-data:/app/data \
  promonitor:latest

# 3) 访问面板 → 管理后台 → 延迟节点 → 添加探测节点（id/name/ip/port）
#   被控会自动从主控拉取节点清单，无需在环境变量配置
```

**若使用 ICMP 延迟探测（`PING_TYPE=icmp`）**：Alpine 容器默认无 `CAP_NET_RAW`，
raw socket 无法建立、ICMP 探测会全部失败，必须给容器显式加上该能力：

```bash
docker run -d --name promonitor --restart unless-stopped \
  -p 9000:9000 \
  --cap-add NET_RAW \          # ← ICMP 模式必需
  -e HMAC_SECRET='<与所有被控一致的随机长串>' \
  -e ADMIN_PASS='<初始管理员密码>' \
  -e SESSION_SECRET='<随机串>' \
  -e PING_TYPE=icmp \
  -v promonitor-data:/app/data \
  promonitor:latest
```

> 提示：`docker run` 无法直接读取 `.env`，密钥用 `-e KEY=value` 逐项传入；
> 嫌长可用 `--env-file .env`（此时 `.env` 需含 `HMAC_SECRET`、`ADMIN_PASS`、`PING_TYPE` 等）。

停止 / 查看日志 / 升级：
```bash
docker stop promonitor && docker rm promonitor   # 停止并移除容器（数据在 promonitor-data 卷里保留）
docker logs -f promonitor                        # 查看日志
docker build -t promonitor:latest . && docker run ...   # 拉新代码后重新构建再启动
```

### 方式 B：docker compose（备选）

```bash
# 1) 准备环境变量
cp .env.example .env
#   编辑 .env，至少填入 HMAC_SECRET（被控/主控必须一致）与 ADMIN_PASS

# 2) 构建并启动（Alpine 3.21 运行镜像，内部用 golang:1.23-alpine 构建）
docker compose up -d --build

# 3) 访问面板
#   http://<你的服务器IP>:9000
#   首次用 admin / 你在 .env 设置的 ADMIN_PASS 登录
```

> compose 使用 ICMP 时同样需要加能力：在 `docker-compose.yml` 的 `promonitor` 服务下加一行
> `cap_add: [NET_RAW]`（并设置 `PING_TYPE=icmp`）。
> **节点清单**在管理后台 → 延迟节点 维护，不通过 `.env` 配置。

- 数据持久化在名为 `promonitor-data` 的卷（`/app/data`），重建容器不丢数据。
- 想改端口：`docker-compose.yml` 的 `ports` 改成 `"8080:9000"` 之类。
- 公网 HTTPS：在容器前再放一个 nginx 反代并配证书（主控本身不内置 TLS）。

---

## 被控 Agent 部署（每台被监控机）

Agent 是独立二进制，跑在**被监控的机器**上，不是面板容器里。

### 方式 A：docker run（推荐）

用同一个 `promonitor` 镜像，以 `promonitor-agent` 覆盖默认命令启动（无需另装二进制）：

```bash
# TCP 延迟探测（默认，跟随主控 PING_TYPE 配置）
docker run -d --name promonitor-agent --restart unless-stopped \
  --network host \
  -e MASTER_URL='http://<主控IP>:9000' \
  -e HMAC_SECRET='<与主控一致的 HMAC_SECRET>' \
  -e SERVER_ID='web-01' \
  -e SERVER_NAME='阿里云-杭州' \
  -e SERVER_IP='10.0.0.5' \
  promonitor:latest promonitor-agent
```

**若主控配置了 ICMP 延迟探测**：被控容器同样需要 `--cap-add NET_RAW`，否则 ICMP 探测全部失败：

```bash
docker run -d --name promonitor-agent --restart unless-stopped \
  --network host \
  --cap-add NET_RAW \        # ← ICMP 模式必需
  -e MASTER_URL='http://<主控IP>:9000' \
  -e HMAC_SECRET='<与主控一致的 HMAC_SECRET>' \
  -e SERVER_ID='web-01' \
  -e SERVER_NAME='阿里云-杭州' \
  -e SERVER_IP='10.0.0.5' \
  promonitor:latest promonitor-agent
```

> `--network host`：让容器直接使用宿主机网络，便于访问主控并让探测流量走宿主网络栈
> （探测目标通常是公网，无需 host 也可，但 host 模式最简单）。节点清单由主控
> 管理后台维护，被控**无需**再配置 `PING_*`。

### 方式 B：直接跑二进制

取二进制（二选一）：
- 直接用仓库 `bin/promonitor-agent-linux-amd64`（arm64 用 `...-arm64`）。
- 或从镜像里拷出：`docker create --name tmp promonitor:latest && docker cp tmp:/usr/local/bin/promonitor-agent ./ && docker rm tmp`。

运行（环境变量或命令行 flag 均可）：
```bash
promonitor-agent \
  -master https://your-panel.example.com \
  -secret "<与主控一致的 HMAC_SECRET>" \
  -id web-01 \
  -name "阿里云-杭州" \
  -ip 10.0.0.5 \
  -interval 30s
```
> **延迟节点与方法由主控统一下发**：Agent 启动即拉取 `GET /api/ping-config`，
> 之后每 5 分钟重新拉取（热更新），**无需在被控侧配置探测目标**。
> `--targets`（或 `PING_TARGETS`）仅作为主控未配置任何节点时的**可选回退覆盖**。

flag 与环境变量对照：

| flag         | 环境变量              | 说明 |
|--------------|----------------------|------|
| `-master`    | `MASTER_URL`         | 主控地址（https 需目标机有 CA 证书）|
| `-secret`    | `HMAC_SECRET`        | HMAC 预共享密钥（**必填**，与主控一致）|
| `-id`        | `SERVER_ID`          | 被控唯一 ID（**必填**，管理页增删时保持一致）|
| `-name`      | `SERVER_NAME`        | 展示名，默认同 ID |
| `-ip`        | `SERVER_IP`          | 被控 IP（可选）|
| `-targets`   | `PING_TARGETS`       | **可选**：逗号分隔，最多 50 个节点；仅当主控未配置节点时回退使用 |
| `-interval`  | `COLLECT_INTERVAL`   | 实时采集间隔（秒），默认 30，最低 30，且必须是 30 的倍数 |

**实时链路**：每 `COLLECT_INTERVAL` 秒采集 CPU/内存/硬盘/网络/Ping 并 `POST /api/ingest`，
主控只刷新 latest_snapshot，**失败不重试**（只有最新数据有价值）。

**历史链路**：被控在本地按整数 10 分钟边界（0/10/20/... 分）聚合，通过 `POST /api/history` 上报；
失败则持久化到本地 SQLite (`agent.db`)，后台自动重试，成功后才删除。网络恢复后历史数据会自动续传。

被控**首次上报即自动注册**到主控（无需先在管理页添加）。

---

## 数据源 / 实时展示

- 列表页、详情页读取 `latest_snapshot`（每被控最新一组指标），前端每 **10s** 轮询一次。
- 详情页历史曲线读取 `metrics_agg`（每 10 分钟 1 行均值），保留 30 天。
- 实时高频上报**只刷新 latest_snapshot，不落历史库**；历史聚合由被控在本地完成后再上报，对服务端资源极友好。

---

## 环境变量（主控）

| 变量 | 必填 | 默认 | 说明 |
|------|------|------|------|
| `PORT`         | 否 | `9000`  | 监听端口 |
| `DB_PATH`      | 是* | —      | SQLite 文件路径（容器内默认 `/app/data/promonitor.db`）|
| `HMAC_SECRET`  | 是 | —      | 被控上报验签密钥（为空则启动报错）|
| `ADMIN_USER`   | 否 | `admin` | 管理员账号 |
| `ADMIN_PASS`   | 否 | —      | 首次启动以此初始化 admin 密码（docker 推荐通过 .env 设置）|
| `SESSION_SECRET` | 否 | `change-me-session-secret` | 会话 Cookie 签名密钥 |
| `FRONTEND_DIR` | 否 | `./dist` | 前端静态目录 |
| `PING_TYPE`    | 否 | `tcp`   | 延迟探测方式：`tcp`（TCP 端口探测）或 `icmp`（需容器 `--cap-add NET_RAW`）|

> 节点清单不再通过环境变量维护，部署后进入 **管理后台 → 延迟节点** 添加（存 SQLite `ping_nodes` 表）。

> `DB_PATH`/`HMAC_SECRET` 在代码中缺失会 `log.Fatal` 退出，属"fail fast"。

---

## 关于 Go 版本的说明（重要）

本项目**源码构建需要 Go ≥ 1.20**，原因是纯 Go 的 SQLite 驱动 `modernc.org/sqlite` 与
`gopsutil/v4` 的最低要求分别为 1.20 / 1.18。因此：

- ❌ 无法用 debian 11 默认软件源的 Go 1.15 **从源码直接构建**（驱动底层卡死，非业务代码问题）。
- ✅ **Docker 部署完全规避此问题**：构建发生在 `golang:1.23-alpine` builder 阶段，运行期是
  `alpine:3.21` 只放 `CGO_ENABLED=0` 静态二进制，**目标机不需要安装任何版本的 Go**。
- ✅ 若不使用 Docker、直接在目标机跑二进制：直接用仓库 `bin/` 下已编译好的
  `linux-amd64` / `linux-arm64` 静态二进制即可，同样不需要目标机装 Go。

源码层面已尽量老 Go 友好：去掉了 `//go:embed`（改用内联 SQL 字符串）、`go.mod` 的
`go` 指令设为 `1.18`；真正构建下限由上述依赖决定为 1.20。

---

## Agent 本地数据库

被控会在启动目录生成 `agent.db`（SQLite），用于持久化待上传的 10 分钟聚合记录。
如果容器化部署，建议把 `agent.db` 所在目录挂载到宿主机或卷，避免容器重启丢失未上传的历史数据。

## 源码本地构建（可选）

```bash
# 后端（需要 Go >= 1.20）
cd server && CGO_ENABLED=0 go build -o ../bin/promonitor-server ./cmd/server
cd ../agent && CGO_ENABLED=0 go build -o ../bin/promonitor-agent .

# 前端
npm install && npm run build   # 产出 dist/

# 本地冒烟测试（先起 server，再跑）
PORT=9000 DB_PATH=./test.db HMAC_SECRET=testsecret ADMIN_PASS=admin123 \
  FRONTEND_DIR=./dist ./bin/promonitor-server &
node scripts/smoke.mjs
```

冒烟脚本覆盖：HMAC 验签、匿名列表、首次上报自动注册、鉴权拦截(401)、登录、增删被控、
改密（旧密码失效/新密码可用）、登出、/api/history 批量聚合上报、历史接口 —— 全绿即通过。
