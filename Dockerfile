# syntax=docker/dockerfile:1

# 纯运行期镜像：Go 二进制与前端 dist 全部由 GitHub Actions 编译产出，
# 镜像内不执行任何编译（无 golang/node 构建阶段）。
#   - bin/promonitor-{server,agent}_linux_amd64  由 build-binaries 工作流交叉编译
#   - dist/                                      由 docker-image 工作流的前端 job 构建
# CI 在 docker build 前把两份产物下载到构建上下文，这里仅做组装。
# 注意：本 Dockerfile 仅供 CI 使用，本地 docker build 需先自行准备 bin/ 与 dist/。

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
ENV PORT=9000 \
    DB_PATH=/app/data/promonitor.db \
    FRONTEND_DIR=/app/dist \
    HMAC_SECRET="" \
    ADMIN_PASS="" \
    SESSION_SECRET="change-me-session-secret"
# CI 预编译的静态二进制（CGO_ENABLED=0，可直接跑在 alpine/musl 上）
COPY bin/promonitor-server_linux_amd64 /usr/local/bin/promonitor-server
COPY bin/promonitor-agent_linux_amd64  /usr/local/bin/promonitor-agent
# CI 构建的前端产物
COPY dist/ /app/dist
EXPOSE 9000
VOLUME ["/app/data"]
CMD ["promonitor-server"]
