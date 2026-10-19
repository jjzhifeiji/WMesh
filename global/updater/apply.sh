#!/bin/sh
# 等确认落下的 app tar，docker load 后按本机架构只重建 app；没有对应架构或探活失败则切回 previous。
# 周期写本机 app 镜像占用；清镜像无论成败都写结果，不挡在已落下的 pending 上。
set -eu
DIR="${WMESH_UPDATE_DIR:-/var/lib/wmesh/update}"
SERVICE="${APP_SERVICE:-app}"
COMPOSE="${COMPOSE_FILE:-/work/docker-compose.yml}"
PROJECT="${COMPOSE_PROJECT_NAME:-wmesh-global}"
# 只是起步值：每轮按宿主此刻的 .env 现算（refresh_app_image），容器创建时定死的环境变量会跟 .env 对不上。
APP_IMAGE="${APP_IMAGE:-wmesh-global-app:dev}"
APP_KIND="${APP_KIND:-wan_service}"
PREV_IMAGE="${PROJECT}-app:previous"
# 核过的包拷到本容器私有目录再 load：更新目录 app 也能写，核完被换包就白核了。
WORK=/tmp/wmesh-apply

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

# 开工时读出这次要换的种类、版本和摘要；pending.tar 不提前删，留给下次确认覆盖。
read_pending() {
  meta=$(tr -d '\n\r' < "$DIR/pending.json" 2>/dev/null || true)
  cur_kind=$(printf '%s' "$meta" | sed -n 's/.*"kind":"\([^"]*\)".*/\1/p')
  cur_version=$(printf '%s' "$meta" | sed -n 's/.*"version":\([0-9][0-9]*\).*/\1/p')
  cur_digest=$(printf '%s' "$meta" | sed -n 's/.*"digest":"\([^"]*\)".*/\1/p')
  [ -n "$cur_version" ] || cur_version=0
}

# 写换包结果，带上开工时读到的种类、版本和摘要；app 只按结果里这一版记已装。
result() {
  res=$(printf '{"ok":%s,"kind":"%s","version":%s,"digest":"%s"' "$1" "${cur_kind:-}" "${cur_version:-0}" "${cur_digest:-}")
  if [ -n "${2:-}" ]; then
    res=$(printf '%s,"error":"%s"' "$res" "$2")
  fi
  status "$res}"
}

# 原子落盘给 app 读，失败也不让主循环退出。
write_file() {
  path=$1
  body=$2
  printf '%s' "$body" > "${path}.tmp" || return 0
  mv "${path}.tmp" "$path" || return 0
  chown 10001:10001 "$path" || true
}

# 按宿主此刻的 .env 现算 app 镜像名，和 compose_up 读同一份配置；宿主改了 WMESH_VERSION / WMESH_REGISTRY 不必重建 updater。
# .env 和 compose 都没变就沿用上次结果，免得每轮都跑 compose config；算不出就沿用上次的（起步是容器环境里的值）。
# compose config --images 会连依赖服务一起列且不保证先后，所以从 app 自己那段取 image。
refresh_app_image() {
  sum=$(cat /work/.env "$COMPOSE" 2>/dev/null | cksum)
  [ "$sum" != "${app_image_sum:-}" ] || return 0
  img=$(docker compose --env-file /work/.env -f "$COMPOSE" -p "$PROJECT" config "$SERVICE" 2>/dev/null | awk -v svc="$SERVICE" '
    $0 == "  " svc ":" { inside = 1; next }
    inside && /^ ? ?[^ ]/ { exit }
    inside && /^    image:[[:space:]]/ { sub(/^    image:[[:space:]]*/, ""); gsub(/["\047]/, ""); print; exit }' || true)
  [ -n "$img" ] || return 0
  APP_IMAGE=$img
  app_image_sum=$sum
}

# app 镜像去掉标签后的仓库名；仓库地址带端口（如 reg.local:5000/）时只去最后的 :标签。
app_repo() {
  printf '%s' "$APP_IMAGE" | sed 's/:[^:/]*$//'
}

# 仓库名按 Docker 规则补全（短名补 docker.io/library/），长短两种写法才能比对。
norm_repo() {
  nr=$1
  case "$nr" in
    */*) ;;
    *) nr="library/$nr" ;;
  esac
  case "${nr%%/*}" in
    *.* | *:* | localhost) ;;
    *) nr="docker.io/$nr" ;;
  esac
  case "$nr" in
    docker.io/*/*) ;;
    docker.io/*) nr="docker.io/library/${nr#docker.io/}" ;;
  esac
  printf '%s' "$nr"
}

# 只放行 app 仓库下带普通标签的引用；打包脚本打的标签不带仓库地址，同名短仓库也算。
# 摘要引用、没标签的一律不放：tar 里带 docker:27-cli 之类标签，下次重建 updater 就是 root 加 docker.sock。
ref_allowed() {
  ra_ref=$1
  case "$ra_ref" in
    *@* | *[!A-Za-z0-9_.:/-]*) return 1 ;;
  esac
  ra_tag=${ra_ref##*:}
  ra_name=${ra_ref%:*}
  case "$ra_ref" in
    *:*) ;;
    *) return 1 ;;
  esac
  case "$ra_tag" in
    '' | */* | [.-]*) return 1 ;;
  esac
  [ "${#ra_tag}" -le 128 ] || return 1
  ra_repo=$(app_repo)
  ra_got=$(norm_repo "$ra_name")
  [ "$ra_got" = "$(norm_repo "$ra_repo")" ] || [ "$ra_got" = "$(norm_repo "${ra_repo##*/}")" ]
}

# OCI 的 ref.name 一般只写标签；只由标签字符组成就不是仓库名，不会落到别的镜像上。
bare_tag() {
  case "$1" in
    '' | [.-]* | *[!A-Za-z0-9_.-]*) return 1 ;;
  esac
}

# 取 JSON 里挂在指定键下的全部字符串，键不分大小写（Docker 解析清单也不分）；结构不对返回失败。
# 调用前已挡掉反斜杠和非 ASCII，字符串里不会有转义的引号，也不会有大小写折叠出的同形键。
json_strings() {
  awk -v keys="$2" '
    BEGIN { n = split(keys, k, ","); for (i = 1; i <= n; i++) want[k[i]] = 1; d = 0 }
    {
      s = $0
      while (s != "") {
        c = substr(s, 1, 1)
        if (c == " " || c == "\t" || c == "\r") { s = substr(s, 2); continue }
        if (c == "\"") {
          s = substr(s, 2)
          i = index(s, "\"")
          if (i == 0) { bad = 1; exit }
          v = substr(s, 1, i - 1)
          s = substr(s, i + 1)
          if (d > 0 && typ[d] == "o" && wantkey[d]) { cur[d] = tolower(v); wantkey[d] = 0; continue }
          own = ""
          if (d > 0) own = (typ[d] == "o") ? cur[d] : owner[d]
          if (own != "" && (own in want)) print v
          continue
        }
        if (c == "{" || c == "[") {
          own = ""
          if (d > 0) own = (typ[d] == "o") ? cur[d] : owner[d]
          d++
          typ[d] = (c == "{") ? "o" : "a"
          owner[d] = own
          cur[d] = ""
          wantkey[d] = (c == "{")
          s = substr(s, 2)
          continue
        }
        if (c == "}" || c == "]") { d--; if (d < 0) { bad = 1; exit } s = substr(s, 2); continue }
        if (c == ",") { if (d > 0 && typ[d] == "o") wantkey[d] = 1; s = substr(s, 2); continue }
        if (c == ":") { s = substr(s, 2); continue }
        if (match(s, /^[-+.0-9A-Za-z]+/)) { s = substr(s, RLENGTH + 1); continue }
        bad = 1
        exit
      }
    }
    END { if (bad || d != 0) exit 1 }' "$1"
}

# 镜像清单只许可打印 ASCII，且不带反斜杠：转义和 Unicode 折叠都能把标签藏过下面的比对。
plain_json() {
  [ "$(tr -d '\t\n\r -~' < "$1" | wc -c)" -eq 0 ] || return 1
  [ "$(tr -cd '\\' < "$1" | wc -c)" -eq 0 ]
}

# 把 pending.tar 拷进私有目录，按 pending.json 摘要核对，再查包里会被打上的全部标签。
# 名字只来自 manifest.json 的 RepoTags 和 index.json 的两个标注；没有 manifest.json 会走旧格式，不认。
# 通过则 $WORK/pending.tar 可 load；不通过打出英文原因并返回失败，私有目录已清掉。
stage_tar() {
  rm -rf "$WORK"
  if ! mkdir -m 0700 "$WORK" "$WORK/meta"; then
    printf '%s' "stage dir failed"
    return 1
  fi
  if [ -L "$DIR/pending.tar" ] || ! cp "$DIR/pending.tar" "$WORK/pending.tar"; then
    rm -rf "$WORK"
    printf '%s' "stage copy failed"
    return 1
  fi
  want=$(printf '%s' "${cur_digest:-}" | base64 -d 2>/dev/null | od -An -tx1 | tr -d ' \n' || true)
  got=$(sha256sum "$WORK/pending.tar" | awk '{print $1}' || true)
  if [ -z "$want" ] || [ "$got" != "$want" ]; then
    rm -rf "$WORK"
    printf '%s' "digest mismatch"
    return 1
  fi
  # 条目去掉 ./ 和重复斜杠后再认；带 .. 或绝对路径、清单重复或缺 manifest.json 都整包不认。
  # busybox 列条目时会自己剥掉 / 和 ../ 前缀并告警，剥过的名字和 Docker 看到的不一样，有告警也不认。
  if ! tar -tf "$WORK/pending.tar" > "$WORK/list" 2> "$WORK/list.err" || [ -s "$WORK/list.err" ] || ! awk '
    {
      if (substr($0, 1, 1) == "/") { bad = 1; exit }
      n = split($0, p, "/")
      out = ""
      for (i = 1; i <= n; i++) {
        if (p[i] == "" || p[i] == ".") continue
        if (p[i] == "..") { bad = 1; exit }
        out = (out == "") ? p[i] : out "/" p[i]
      }
      if (out == "manifest.json" || out == "index.json") { cnt[out]++; print out "\t" $0 }
    }
    END { if (bad || cnt["manifest.json"] != 1 || cnt["index.json"] > 1) exit 1 }' "$WORK/list" > "$WORK/names"; then
    rm -rf "$WORK"
    printf '%s' "bad image archive"
    return 1
  fi
  tab=$(printf '\t')
  while IFS="$tab" read -r name raw; do
    # 单独解出这一条再看类型：软链、硬链、目录 Docker 会读到别处的内容，一律不认。
    if ! tar -xf "$WORK/pending.tar" -C "$WORK/meta" "$raw" 2>/dev/null || [ -L "$WORK/meta/$name" ] || [ ! -f "$WORK/meta/$name" ] || ! plain_json "$WORK/meta/$name"; then
      rm -rf "$WORK"
      printf '%s' "bad image archive"
      return 1
    fi
  done < "$WORK/names"
  if ! json_strings "$WORK/meta/manifest.json" repotags > "$WORK/tags"; then
    rm -rf "$WORK"
    printf '%s' "bad image archive"
    return 1
  fi
  if [ -f "$WORK/meta/index.json" ] && ! json_strings "$WORK/meta/index.json" io.containerd.image.name,org.opencontainers.image.ref.name > "$WORK/refs"; then
    rm -rf "$WORK"
    printf '%s' "bad image archive"
    return 1
  fi
  : >> "$WORK/refs"
  while IFS= read -r ref; do
    if ! ref_allowed "$ref"; then
      rm -rf "$WORK"
      printf '%s' "foreign image tag"
      return 1
    fi
  done < "$WORK/tags"
  while IFS= read -r ref; do
    if ! bare_tag "$ref" && ! ref_allowed "$ref"; then
      rm -rf "$WORK"
      printf '%s' "foreign image tag"
      return 1
    fi
  done < "$WORK/refs"
  rm -rf "$WORK/meta" "$WORK/list" "$WORK/list.err" "$WORK/names" "$WORK/tags" "$WORK/refs"
  # 记下 load 前全部标签和镜像 ID，load 后万一多出外来标签好改回去。
  docker images --no-trunc --format '{{.Repository}}:{{.Tag}} {{.ID}}' > "$WORK/before" 2>/dev/null || true
}

# load 后再按 Docker 自己报的标签核一遍，防 tar 解析差异漏过上面的检查。
# app 仓库以外的标签改回 load 前的镜像，原来没有就去掉；有过就返回失败。
undo_foreign() {
  uf_bad=""
  for uf_ref in $(printf '%s\n' "$1" | sed -n 's/^Loaded image: //p'); do
    ref_allowed "$uf_ref" && continue
    uf_bad=1
    uf_old=$(awk -v r="$uf_ref" '$1 == r { print $2; exit }' "$WORK/before" 2>/dev/null || true)
    if [ -n "$uf_old" ]; then
      docker tag "$uf_old" "$uf_ref" || true
    else
      docker rmi "$uf_ref" >/dev/null 2>&1 || true
    fi
  done
  [ -z "$uf_bad" ]
}

compose_up() {
  # 只重建 app，但仍要读 .env 才能解析 compose 里的必填变量。
  # /work 是整个工程目录，带 --no-build 免得镜像缺失时在 updater 里按宿主源码现编一份。
  docker compose --env-file /work/.env -f "$COMPOSE" -p "$PROJECT" up -d --no-deps --no-build --force-recreate --wait --wait-timeout 90 "$SERVICE"
}

# app 的绑定挂载源落在 /work 下就不能换：这是 updater 里的路径，宿主机上没有，换完 app 会丢证书目录。
# 只看 app 自己那段，updater 自己挂的 /work 不算；不安全就打出英文原因并返回失败。
compose_safe() {
  if ! cfg=$(docker compose --env-file /work/.env -f "$COMPOSE" -p "$PROJECT" config "$SERVICE" 2>/dev/null); then
    printf '%s' "compose config failed"
    return 1
  fi
  if ! printf '%s\n' "$cfg" | awk -v svc="$SERVICE" '
    $0 == "  " svc ":" { inside = 1; next }
    inside && /^ ? ?[^ ]/ { inside = 0 }
    inside && /^[[:space:]]+source:[[:space:]]+\/work(\/|$)/ { bad = 1 }
    END { exit bad }'; then
    printf '%s' "app bind mount under /work; use absolute host path"
    return 1
  fi
}

# 把换包前的 current 记成 previous；它已是这次要装的包（上次换到一半被打断）就不覆盖，留住真正的旧版。
mark_previous() {
  old_id=$1
  new_id=$(docker image inspect -f '{{.Id}}' "$2" 2>/dev/null || true)
  [ -n "$old_id" ] || return 0
  [ "$old_id" != "$new_id" ] || return 0
  docker tag "$old_id" "$PREV_IMAGE" || true
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
    # load 前已核过标签，这里再挡一道，别把 app 仓库以外的镜像打成 app。
    ref_allowed "$ref" || continue
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
  repo=$(app_repo)
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
  repo=$(app_repo)
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
  repo=$(app_repo)
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
  # 先按宿主此刻的 .env 定下这一轮的 app 镜像名，清单、清镜像、换包和回退都用它。
  refresh_app_image
  tick_images
  # 挂载不安全就这次不 load、不换，正在跑的 app 不动；写了结果就不会再进下面的换包。
  if [ -f "$DIR/pending.json" ] && [ -f "$DIR/pending.tar" ] && [ ! -f "$DIR/status.json" ]; then
    if ! unsafe=$(compose_safe); then
      read_pending
      result false "$unsafe"
    fi
  fi
  if [ -f "$DIR/pending.json" ] && [ -f "$DIR/pending.tar" ] && [ ! -f "$DIR/status.json" ]; then
    read_pending
    # load 前先按 ID 记下 current，previous 等 load 完比过再定，防止被打断重来时把新包记成旧版。
    cur_id=$(docker image inspect -f '{{.Id}}' "$APP_IMAGE" 2>/dev/null || true)
    # 摘要不符或带 app 仓库以外的标签就不 load；只 load 核过的私有副本。
    if ! bad=$(stage_tar); then
      result false "${bad:-bad image archive}"
    elif out=$(docker load -i "$WORK/pending.tar"); then
      img=$(pick_from_load "$out")
      if ! undo_foreign "$out"; then
        result false "foreign image tag"
      elif [ -z "$img" ]; then
        result false "$(printf 'no image for %s/%s' "$(engine_os)" "$(engine_arch)")"
      else
        mark_previous "$cur_id" "$img"
        docker tag "$img" "$APP_IMAGE" || true
        if compose_up; then
          result true
        else
          revert
          result false "health check failed"
        fi
      fi
    else
      # load 失败前可能已打上部分标签，同样核一遍。
      undo_foreign "${out:-}" || true
      result false "docker load failed"
    fi
    rm -rf "$WORK"
    write_images || true
  fi
  sleep 2
done
