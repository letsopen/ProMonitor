# syntax=docker/dockerfile:1

# ========== 1. 后端构建（主控 + 被控，纯静态二进制） ==========
FROM golang:1.23-alpine AS gobuilder
WORKDIR /src
# 先拷贝 go.mod/go.sum 并下载依赖，充分利用构建缓存
COPY server/go.mod server/go.sum ./server/
COPY agent/go.mod agent/go.sum ./agent/
RUN cd server && go mod download
RUN cd agent && go mod download
COPY server/ ./server/
COPY agent/ ./agent/
# CGO_ENABLED=0 生成不依赖 libc 的静态二进制，可直接跑在 alpine/musl 上
RUN cd server && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/promonitor-server .
RUN cd agent  && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/promonitor-agent .

# ========== 2. 前端构建 ==========
FROM node:22-alpine AS webuilder
WORKDIR /web
# 复制 lock 文件并用 npm ci：保证 CI 与本地依赖树完全一致、可复现构建
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
RUN npm run build

# ========== 3. 运行期（Alpine 3.21） ==========
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
ENV PORT=9000 \
    DB_PATH=/app/data/promonitor.db \
    FRONTEND_DIR=/app/dist \
    HMAC_SECRET="" \
    ADMIN_PASS="" \
    SESSION_SECRET="change-me-session-secret"
COPY --from=gobuilder /out/promonitor-server /usr/local/bin/promonitor-server
COPY --from=gobuilder /out/promonitor-agent  /usr/local/bin/promonitor-agent
COPY --from=webuilder /web/dist /app/dist
EXPOSE 9000
VOLUME ["/app/data"]
CMD ["promonitor-server"]
