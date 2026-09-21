#!/usr/bin/env bash
# 本机按当前源码重建并拉起 docker 栈。不加版本、不打更新包、不拉基础镜像。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE="${COMPOSE:-docker compose}"

usage() {
  cat <<'EOF'
用法: scripts/dev-up.sh [wan|factory|all]

  all（默认）  云端 + 厂端
  wan          只云端  http://localhost:52080
  factory      只厂端  http://localhost:52081

有 .env 就沿用，没有则从 .env.example 复制。改过源码要本机页面跟上，跑这一条。
EOF
}

die() { echo "dev-up: $*" >&2; exit 1; }

need_cmd() { command -v "$1" >/dev/null 2>&1 || die "需要命令 $1"; }

http_port() {
  local envf="$1/.env"
  local def="$2"
  if [[ -f "$envf" ]]; then
    local v
    v="$(grep -E '^HTTP_PORT=' "$envf" | tail -n1 | cut -d= -f2- || true)"
    [[ -n "$v" ]] && printf '%s' "$v" && return
  fi
  printf '%s' "$def"
}

up_side() {
  local dir="$1"
  local name="$2"
  local def_port="$3"
  [[ -d "$ROOT/$dir" ]] || die "没有 $dir/"
  echo ">> ${name}：按当前源码重建"
  make -C "$ROOT/$dir" up
  local port
  port="$(http_port "$ROOT/$dir" "$def_port")"
  local url="http://127.0.0.1:${port}"
  local i
  for i in 1 2 3 4 5 6 7 8 9 10; do
    if curl -fsS "$url/healthz" >/dev/null 2>&1; then
      echo ">> ${name} 就绪  $url"
      return
    fi
    sleep 1
  done
  die "${name} 已起容器，但 $url/healthz 无响应"
}

SIDES=()
for arg in "$@"; do
  case "$arg" in
    -h | --help)
      usage
      exit 0
      ;;
    all)
      SIDES=(wan factory)
      ;;
    wan | global)
      SIDES+=(wan)
      ;;
    factory | fac)
      SIDES+=(factory)
      ;;
    *)
      usage >&2
      die "未知参数：$arg"
      ;;
  esac
done

if [[ ${#SIDES[@]} -eq 0 ]]; then
  SIDES=(wan factory)
fi

need_cmd make
need_cmd docker
need_cmd curl
# COMPOSE 允许写成 docker compose 或 docker-compose
# shellcheck disable=SC2086
$COMPOSE version >/dev/null 2>&1 || die "需要 Docker Compose v2"

# 去重且保持 wan 在 factory 前。
SEEN_WAN=0
SEEN_FAC=0
for s in "${SIDES[@]}"; do
  case "$s" in
    wan) SEEN_WAN=1 ;;
    factory) SEEN_FAC=1 ;;
  esac
done

[[ "$SEEN_WAN" -eq 1 ]] && up_side global 云端 52080
[[ "$SEEN_FAC" -eq 1 ]] && up_side factory 厂端 52081
