# 入口 + 静态页 + 本侧 Go，SIDE=global 或 factory。
ARG SIDE=global

FROM node:22-alpine AS frontend
ARG SIDE
WORKDIR /src
COPY ${SIDE}/frontend/package.json ${SIDE}/frontend/package-lock.json ./
RUN npm ci
COPY ${SIDE}/frontend/ ./
RUN npm run build

FROM golang:1.26-bookworm AS server
ARG SIDE
WORKDIR /src
COPY ${SIDE}/server/go.mod ${SIDE}/server/go.sum ./
RUN go mod download
COPY ${SIDE}/server/ ./
RUN CGO_ENABLED=0 go build -o /out/wmesh ./cmd/server

FROM nginx:1.27-alpine
RUN apk add --no-cache wget
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY deploy/app-start.sh /usr/local/bin/app-start.sh
COPY --from=frontend /src/dist /usr/share/nginx/html
COPY --from=server /out/wmesh /usr/local/bin/wmesh
RUN chmod +x /usr/local/bin/app-start.sh
ENV WMESH_HTTP_ADDR=:8080
EXPOSE 80
CMD ["/usr/local/bin/app-start.sh"]
