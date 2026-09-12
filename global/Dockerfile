# WAN 服务镜像：前端构建 → Go 静态编译 → 极简运行时。单进程同时提供 API 与管理端静态页。
# 构建上下文是本目录（global/）：docker build -t wmesh-global-app .

FROM node:24-alpine AS web
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-alpine AS server
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY server/ ./
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/wmesh ./cmd/server

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 wmesh
COPY --from=server /out/wmesh /usr/local/bin/wmesh
COPY --from=web /src/dist /srv/web
USER wmesh
ENV WMESH_HTTP_ADDR=:8080 \
    WMESH_WEB_DIR=/srv/web
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=6 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/wmesh"]
