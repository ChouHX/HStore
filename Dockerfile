# ---------- 前端构建 ----------
FROM node:24-alpine AS web

WORKDIR /app

COPY package.json package-lock.json ./
COPY scripts ./scripts
RUN npm ci

COPY index.html vite.config.ts tsconfig.json ./
COPY src ./src
COPY components ./components
COPY lib ./lib
COPY public ./public

# 产物直接写入 backend/web/dist，供 Go 的 go:embed 使用
COPY backend ./backend
RUN npm run build:web


# ---------- Go 编译 ----------
FROM golang:1.23-alpine AS build

WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend ./
COPY --from=web /app/backend/web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/hstore-server \
    ./cmd/server


# ---------- 运行镜像 ----------
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app app \
    && mkdir -p /data \
    && chown app:app /data

COPY --from=build /out/hstore-server /usr/local/bin/hstore-server

ENV PORT=3001 \
    FAKA_ADDR=0.0.0.0 \
    FAKA_DB_PATH=/data/faka.db \
    TZ=Asia/Shanghai

WORKDIR /app
USER app

VOLUME ["/data"]
EXPOSE 3001

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:3001/api/health || exit 1

ENTRYPOINT ["hstore-server"]
