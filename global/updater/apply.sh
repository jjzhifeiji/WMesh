#!/bin/sh
# 等确认落下的 app tar，docker load 后按本机架构只重建 app；没有对应架构或探活失败则切回 previous。
# 周期写本机 app 镜像占用；清镜像无论成败都写结果，不挡在已落下的 pending 上。
set -eu
DIR="${WMESH_UPDATE_DIR:-/var/lib/wmesh/update}"
SERVICE="${APP_SERVICE:-app}"
COMPOSE="${COMPOSE_FILE:-/work/docker-compose.yml}"
PROJECT="${COMPOSE_PROJECT_NAME:-wmesh-global}"
APP_IMAGE="${APP_IMAGE:-wmesh-global-app:dev}"
APP_KIND="${APP_KIND:-wan_service}"
PREV_IMAGE="${PROJECT}-app:previous"

# app 以 uid 10001 写 pending；本进程是 root，目录必须两边都能写。
prepare_dir() {
  mkdir -p "$DIR"
  chown -R 10001:10001 "$DIR"
  chmod 0775 "$DIR"
}

status() {
  printf '%s' "$1" > "$DIR/status.json.tmp"
  mv "$DIR/status.json.tmp" "$DIR/status.json"
  chown 10001:10001 "$DIR/status.json"
}

# 原子落盘给 app 读，失败也不让主循环退出。
write_file() {
  path=$1
  body=$2
  printf '%s' "$body" > "${path}.tmp" || return 0
  mv "${path}.tmp" "$path" || return 0
  chown 10001:10001 "$path" || true
}

compose_up() {
  # 只重建 app，但仍要读 .env 才能解析 compose 里的必填变量。
  docker compose --env-file /work/.env -f "$COMPOSE" -p "$PROJECT" up -d --no-deps --force-recreate --wait --wait-timeout 90 "$SERVICE"
}

engine_os() {
  docker info --format '{{.OSType}}' 2>/dev/null || printf '%s' linux
}

engine_arch() {
  arch=$(docker info --format '{{.Architecture}}' 2>/dev/null || uname -m)
  case "$arch" in
    x86_64 | amd64 | x86-64) printf '%s' amd64 ;;
    aarch64 | arm64 | arm64/v8) printf '%s' arm64 ;;
    *) printf '%s' "$arch" ;;
  esac
}

normalize_arch() {
  case "$1" in
    x86_64 | amd64 | x86-64) printf '%s' amd64 ;;
    aarch64 | arm64 | arm64/v8) printf '%s' arm64 ;;
    *) printf '%s' "$1" ;;
  esac
}

# 只从刚 load 出来的标签里挑本机 Docker 引擎能跑的那份。
pick_from_load() {
  loaded=$1
  want_os=$(engine_os)
  want_arch=$(engine_arch)
  preferred=""
  any=""
  for ref in $(printf '%s\n' "$loaded" | sed -n 's/^Loaded image: //p'); do
    [ -n "$ref" ] || continue
    os=$(docker image inspect -f '{{.Os}}' "$ref" 2>/dev/null || true)
    arch=$(normalize_arch "$(docker image inspect -f '{{.Architecture}}' "$ref" 2>/dev/null || true)")
    [ "$os" = "$want_os" ] && [ "$arch" = "$want_arch" ] || continue
    any=$ref
    case "$ref" in
      *-"$want_os"-"$want_arch") preferred=$ref ;;
      *-"$want_arch") preferred=$ref ;;
    esac
  done
  if [ -z "$any" ]; then
    for id in $(printf '%s\n' "$loaded" | sed -n 's/^Loaded image ID: //p'); do
      [ -n "$id" ] || continue
      os=$(docker image inspect -f '{{.Os}}' "$id" 2>/dev/null || true)
      arch=$(normalize_arch "$(docker image inspect -f '{{.Architecture}}' "$id" 2>/dev/null || true)")
      [ "$os" = "$want_os" ] && [ "$arch" = "$want_arch" ] || continue
      any=$id
    done
  fi
  if [ -n "$preferred" ]; then
    printf '%s' "$preferred"
  else
    printf '%s' "$any"
  fi
}

revert() {
  if docker image inspect "$PREV_IMAGE" >/dev/null 2>&1; then
    docker tag "$PREV_IMAGE" "$APP_IMAGE" || true
    compose_up || true
  fi
}

# 只清无用 app 标签和悬空层；current / previous 和正在跑的基础设施不动。
prune_images() {
  repo=$(printf '%s' "$APP_IMAGE" | sed 's/:.*$//')
  keep_app=$(docker image inspect -f '{{.Id}}' "$APP_IMAGE" 2>/dev/null || true)
  keep_prev=$(docker image inspect -f '{{.Id}}' "$PREV_IMAGE" 2>/dev/null || true)
  docker images --format '{{.Repository}}:{{.Tag}} {{.ID}}' 2>/dev/null | while read -r ref id; do
    case "$ref" in
      "$APP_IMAGE"|"$PREV_IMAGE") continue ;;
      "$repo":*)
        if [ -n "$id" ] && [ "$id" != "$keep_app" ] && [ "$id" != "$keep_prev" ]; then
          docker rmi "$ref" >/dev/null 2>&1 || true
        fi
        ;;
    esac
  done
  docker image prune -f 2>/dev/null || true
}

# 只清点名的 app 标签；current / previous 拒绝。成功无输出。
prune_one() {
  ref=$1
  repo=$(printf '%s' "$APP_IMAGE" | sed 's/:.*$//')
  keep_app=$(docker image inspect -f '{{.Id}}' "$APP_IMAGE" 2>/dev/null || true)
  keep_prev=$(docker image inspect -f '{{.Id}}' "$PREV_IMAGE" 2>/dev/null || true)
  case "$ref" in
    "$APP_IMAGE"|"$PREV_IMAGE")
      printf '%s' "keep current or previous"
      return 1
      ;;
    "$repo":*)
      id=$(docker image inspect -f '{{.Id}}' "$ref" 2>/dev/null || true)
      if [ -z "$id" ]; then
        printf '%s' "image not found"
        return 1
      fi
      if [ "$id" = "$keep_app" ] || [ "$id" = "$keep_prev" ]; then
        printf '%s' "keep current or previous"
        return 1
      fi
      docker rmi "$ref" >/dev/null 2>&1 || true
      return 0
      ;;
  esac
  printf '%s' "not app image"
  return 1
}

# prune.req：ALL=全部可清，否则只清这一条。读不到就停。
prune_req_target() {
  tr -d '\n\r' < "$DIR/prune.req" 2>/dev/null || true
}

# 给 app 看的本机 app 镜像占用；docker 失败也写空清单。
write_images() {
  repo=$(printf '%s' "$APP_IMAGE" | sed 's/:.*$//')
  keep_app=$(docker image inspect -f '{{.Id}}' "$APP_IMAGE" 2>/dev/null || true)
  keep_prev=$(docker image inspect -f '{{.Id}}' "$PREV_IMAGE" 2>/dev/null || true)
  raw="$DIR/images.raw"
  lst="$DIR/images.lst"
  : > "$raw"
  : > "$lst"
  docker images --format '{{.Repository}}\t{{.Tag}}\t{{.ID}}' > "$raw" 2>/dev/null || true
  if [ -s "$raw" ]; then
    while IFS="$(printf '\t')" read -r r t id; do
      [ -n "$r" ] || continue
      [ "$r" = "<none>" ] && continue
      ref="$r:$t"
      case "$ref" in
        "$APP_IMAGE"|"$PREV_IMAGE"|"$repo":*) ;;
        *) continue ;;
      esac
      size=$(docker image inspect -f '{{.Size}}' "$id" 2>/dev/null || echo 0)
      keep=""
      # 同一镜像的其它标签也算当前/上一份，不能当可清。
      full=$(docker image inspect -f '{{.Id}}' "$ref" 2>/dev/null || true)
      [ -n "$full" ] && [ "$full" = "$keep_app" ] && keep=current
      [ -n "$full" ] && [ "$full" = "$keep_prev" ] && keep=previous
      printf '%s\t%s\t%s\t%s\n' "$ref" "$id" "$size" "$keep"
    done < "$raw" > "$lst"
  fi
  used=0
  count=0
  if [ -s "$lst" ]; then
    used=$(awk -F'\t' '{if (!seen[$2]++) s+=$3} END {print s+0}' "$lst")
    count=$(awk -F'\t' '{if (!seen[$2]++) n++} END {print n+0}' "$lst")
  fi
  tmp="$DIR/images.json.tmp"
  printf '{"kind":"%s","used":%s,"count":%s,"items":[' "$APP_KIND" "${used:-0}" "${count:-0}" > "$tmp"
  sep=""
  if [ -s "$lst" ]; then
    while IFS="$(printf '\t')" read -r ref id size keep; do
      printf '%s{"ref":"%s","id":"%s","size":%s,"keep":"%s"}' "$sep" "$ref" "$id" "${size:-0}" "$keep" >> "$tmp"
      sep=","
    done < "$lst"
  fi
  printf ']}' >> "$tmp"
  mv "$tmp" "$DIR/images.json" || true
  chown 10001:10001 "$DIR/images.json" || true
  rm -f "$raw" "$lst"
}

# 正在换 app 时先不清，避免把刚 load 的镜像删掉。
applying() {
  [ -f "$DIR/pending.json" ] && [ -f "$DIR/pending.tar" ] && [ ! -f "$DIR/status.json" ]
}

# 无论 docker 成败都写 prune.json，避免管理台一直转圈。
run_prune() {
  if ! docker info >/dev/null 2>&1; then
    write_file "$DIR/prune.json" '{"ok":false,"reclaimed":"0B","error":"docker unavailable"}'
    rm -f "$DIR/prune.req"
    return 0
  fi
  target=$(prune_req_target)
  if [ "$target" = "ALL" ]; then
    out=$(prune_images 2>/dev/null || true)
    reclaim=$(printf '%s\n' "$out" | sed -n 's/Total reclaimed space: //p' | tail -n 1)
    [ -n "$reclaim" ] || reclaim=0B
    write_file "$DIR/prune.json" "$(printf '{"ok":true,"reclaimed":"%s"}' "$reclaim")"
    rm -f "$DIR/prune.req"
    return 0
  fi
  if [ -z "$target" ]; then
    write_file "$DIR/prune.json" '{"ok":false,"reclaimed":"0B","error":"empty prune request"}'
    rm -f "$DIR/prune.req"
    return 0
  fi
  err=$(prune_one "$target") || true
  if [ -n "$err" ]; then
    write_file "$DIR/prune.json" "$(printf '{"ok":false,"reclaimed":"0B","error":"%s"}' "$err")"
    rm -f "$DIR/prune.req"
    return 0
  fi
  write_file "$DIR/prune.json" '{"ok":true,"reclaimed":"0B"}'
  rm -f "$DIR/prune.req"
}

# 镜像清单和清镜像失败不得打死 updater。
tick_images() {
  set +e
  write_images
  if [ -f "$DIR/prune.req" ] && ! applying; then
    run_prune
    write_images
  fi
  set -e
}

prepare_dir
while true; do
  tick_images
  if [ -f "$DIR/pending.json" ] && [ -f "$DIR/pending.tar" ] && [ ! -f "$DIR/status.json" ]; then
    if docker image inspect "$APP_IMAGE" >/dev/null 2>&1; then
      docker tag "$APP_IMAGE" "$PREV_IMAGE" || true
    fi
    if out=$(docker load -i "$DIR/pending.tar"); then
      img=$(pick_from_load "$out")
      if [ -z "$img" ]; then
        status "$(printf '{"ok":false,"error":"no image for %s/%s"}' "$(engine_os)" "$(engine_arch)")"
      else
        docker tag "$img" "$APP_IMAGE" || true
        if compose_up; then
          status '{"ok":true}'
        else
          revert
          status '{"ok":false,"error":"health check failed"}'
        fi
      fi
    else
      status '{"ok":false,"error":"docker load failed"}'
    fi
    write_images || true
  fi
  sleep 2
done
