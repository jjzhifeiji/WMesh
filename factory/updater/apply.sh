#!/bin/sh
# 等确认落下的 app tar，docker load 后只重建 app；探活失败切回 previous。
# 周期写本机 app 镜像占用；清镜像无论成败都写结果，不挡在已落下的 pending 上。
set -eu
DIR="${WMESH_UPDATE_DIR:-/var/lib/wmesh/update}"
SERVICE="${APP_SERVICE:-app}"
COMPOSE="${COMPOSE_FILE:-/work/docker-compose.yml}"
PROJECT="${COMPOSE_PROJECT_NAME:-wmesh-factory}"
APP_IMAGE="${APP_IMAGE:-wmesh-factory-app:dev}"
APP_KIND="${APP_KIND:-factory_service}"
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

loaded_ref() {
  out=$1
  img=$(printf '%s\n' "$out" | sed -n 's/^Loaded image: //p' | tail -n 1)
  if [ -z "$img" ]; then
    img=$(printf '%s\n' "$out" | sed -n 's/^Loaded image ID: //p' | tail -n 1)
  fi
  printf '%s' "$img"
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

# 给 app 看的本机 app 镜像占用；docker 失败也写空清单。
write_images() {
  repo=$(printf '%s' "$APP_IMAGE" | sed 's/:.*$//')
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
      [ "$ref" = "$APP_IMAGE" ] && keep=current
      [ "$ref" = "$PREV_IMAGE" ] && keep=previous
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
  out=$(prune_images 2>/dev/null || true)
  reclaim=$(printf '%s\n' "$out" | sed -n 's/Total reclaimed space: //p' | tail -n 1)
  [ -n "$reclaim" ] || reclaim=0B
  write_file "$DIR/prune.json" "$(printf '{"ok":true,"reclaimed":"%s"}' "$reclaim")"
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
      img=$(loaded_ref "$out")
      if [ -n "$img" ]; then
        docker tag "$img" "$APP_IMAGE" || true
      fi
      if compose_up; then
        status '{"ok":true}'
      else
        revert
        status '{"ok":false,"error":"health check failed"}'
      fi
    else
      status '{"ok":false,"error":"docker load failed"}'
    fi
    write_images || true
  fi
  sleep 2
done
