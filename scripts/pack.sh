#!/usr/bin/env bash
# 按种类打软件更新包：只导出 app 镜像或 APK，不含 db / oss / alloy。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${ROOT}/dist"
COMPOSE="${COMPOSE:-docker compose}"
DO_TEST=0
DO_PULL=0
DO_BUMP=1
KINDS=()
BACKUP_DIR=""

usage() {
  cat <<'EOF'
用法: scripts/pack.sh [种类...] [选项]

种类（可多选；省略则打全部）:
  all                 云端服务 + 厂服务 + 客户端
  wan_service         WAN app 镜像 tar.gz
  factory_service     厂 app 镜像 tar.gz
  client_apk          Android APK

选项:
  --out DIR           输出目录，默认仓库根 dist/
  --test              打包前跑对应工程测试
  --pull              构建镜像时拉取新的基础镜像
  --no-bump           不改版本，按源码现有号打包
  -h, --help

每次打包把该种类版本号 +1、版本名末位 +1，并写回源码（服务端与前端对齐）。
构建失败会改回。文件名:
  dist/<kind>-v<version>-<versionName>.tar.gz|apk
  同名 .json 记种类、版本、SHA-256，供上传。
EOF
}

die() { echo "pack: $*" >&2; exit 1; }

need_cmd() { command -v "$1" >/dev/null 2>&1 || die "需要命令 $1"; }

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# 读 Go release.Code（不读 WebCode）。
parse_go_code() {
  local v
  v="$(sed -nE 's/^[[:space:]]*Code[[:space:]]+int64[[:space:]]*=[[:space:]]*([0-9]+).*/\1/p' "$1" | head -n1)"
  [[ -n "$v" ]] || die "读不到 Code：$1"
  printf '%s' "$v"
}

# 读 Go release.Name（不读 WebName）。
parse_go_name() {
  local v
  v="$(sed -nE 's/^[[:space:]]*Name[[:space:]]*=[[:space:]]*"([^"]+)".*/\1/p' "$1" | head -n1)"
  [[ -n "$v" ]] || die "读不到 Name：$1"
  printf '%s' "$v"
}

parse_gradle_code() {
  local v
  v="$(sed -nE 's/^[[:space:]]*versionCode[[:space:]]*=[[:space:]]*([0-9]+).*/\1/p' "$1" | head -n1)"
  [[ -n "$v" ]] || die "读不到 versionCode：$1"
  printf '%s' "$v"
}

parse_gradle_name() {
  local v
  v="$(sed -nE 's/^[[:space:]]*versionName[[:space:]]*=[[:space:]]*"([^"]+)".*/\1/p' "$1" | head -n1)"
  [[ -n "$v" ]] || die "读不到 versionName：$1"
  printf '%s' "$v"
}

# 打包失败时把已改的版本文件改回。
restore_backup() {
  [[ -n "${BACKUP_DIR:-}" && -d "$BACKUP_DIR" ]] || return 0
  local rel
  while IFS= read -r rel; do
    [[ -n "$rel" ]] || continue
    cp "$BACKUP_DIR/$rel" "$ROOT/$rel"
  done <"$BACKUP_DIR/manifest.txt"
  echo "pack: 打包失败，版本已改回" >&2
}

clear_backup() {
  [[ -n "${BACKUP_DIR:-}" && -d "$BACKUP_DIR" ]] && rm -rf "$BACKUP_DIR"
  BACKUP_DIR=""
}

on_exit() {
  local ec=$?
  trap - EXIT
  if [[ $ec -ne 0 ]]; then
    restore_backup
  fi
  clear_backup
  exit "$ec"
}
trap on_exit EXIT

# 备份即将改写的版本文件。
backup_files() {
  clear_backup
  BACKUP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/wmesh-pack.XXXXXX")"
  : >"$BACKUP_DIR/manifest.txt"
  local f rel
  for f in "$@"; do
    [[ -f "$f" ]] || die "找不到 $f"
    rel="${f#"$ROOT"/}"
    mkdir -p "$BACKUP_DIR/$(dirname "$rel")"
    cp "$f" "$BACKUP_DIR/$rel"
    printf '%s\n' "$rel" >>"$BACKUP_DIR/manifest.txt"
  done
}

# 该种类版本号 +1、版本名末位 +1；打印「code name」。
bump_kind() {
  python3 - "$ROOT" "$1" <<'PY'
import re, sys
from pathlib import Path

root = Path(sys.argv[1])
kind = sys.argv[2]


def bump_name(name: str) -> str:
    parts = name.split(".")
    if parts and parts[-1].isdigit():
        parts[-1] = str(int(parts[-1]) + 1)
        return ".".join(parts)
    return name + ".1"


def sub_count(text, pat, repl):
    out, n = re.subn(pat, repl, text, count=1, flags=re.M)
    if n != 1:
        raise SystemExit(f"pack: 改不到版本字段：{pat}")
    return out


def bump_go(path: Path, web: bool) -> tuple[int, str]:
    text = path.read_text(encoding="utf-8")
    m = re.search(r"^(\s*Code\s+int64\s*=\s*)(\d+)", text, re.M)
    if not m:
        raise SystemExit(f"pack: 读不到 Code：{path}")
    code = int(m.group(2)) + 1
    text = sub_count(text, r"^(\s*Code\s+int64\s*=\s*)\d+", rf"\g<1>{code}")
    m = re.search(r'^(\s*Name\s*=\s*")([^"]+)"', text, re.M)
    if not m:
        raise SystemExit(f"pack: 读不到 Name：{path}")
    name = bump_name(m.group(2))
    text = sub_count(text, r'^(\s*Name\s*=\s*")[^"]+(")', rf"\g<1>{name}\2")
    if web:
        text = sub_count(text, r"^(\s*WebCode\s+int64\s*=\s*)\d+", rf"\g<1>{code}")
        text = sub_count(text, r'^(\s*WebName\s*=\s*")[^"]+(")', rf"\g<1>{name}\2")
    path.write_text(text, encoding="utf-8")
    return code, name


def bump_ts(path: Path, code: int, name: str) -> None:
    text = path.read_text(encoding="utf-8")
    text = sub_count(text, r"^(export const VERSION_CODE = )\d+", rf"\g<1>{code}")
    text = sub_count(text, r'^(export const VERSION_NAME = ")[^"]+(")', rf"\g<1>{name}\2")
    path.write_text(text, encoding="utf-8")


def bump_gradle(path: Path) -> tuple[int, str]:
    text = path.read_text(encoding="utf-8")
    m = re.search(r"^(\s*versionCode\s*=\s*)(\d+)", text, re.M)
    if not m:
        raise SystemExit(f"pack: 读不到 versionCode：{path}")
    code = int(m.group(2)) + 1
    text = sub_count(text, r"^(\s*versionCode\s*=\s*)\d+", rf"\g<1>{code}")
    m = re.search(r'^(\s*versionName\s*=\s*")([^"]+)"', text, re.M)
    if not m:
        raise SystemExit(f"pack: 读不到 versionName：{path}")
    name = bump_name(m.group(2))
    text = sub_count(text, r'^(\s*versionName\s*=\s*")[^"]+(")', rf"\g<1>{name}\2")
    path.write_text(text, encoding="utf-8")
    return code, name


if kind == "wan_service":
    code, name = bump_go(root / "global/server/internal/platform/release/release.go", False)
    bump_ts(root / "global/frontend/src/shared/version.ts", code, name)
elif kind == "factory_service":
    code, name = bump_go(root / "factory/server/internal/platform/release/release.go", True)
    bump_ts(root / "factory/frontend/src/shared/version.ts", code, name)
elif kind == "client_apk":
    code, name = bump_gradle(root / "client/ShiJiaoQi-New/app/build.gradle.kts")
else:
    raise SystemExit(f"pack: 未知种类 {kind}")
print(f"{code} {name}")
PY
}

# 包旁边写上传用元数据。
write_meta() {
  local file="$1" kind="$2" version="$3" name="$4" digest="$5"
  python3 - "$file" "$kind" "$version" "$name" "$digest" <<'PY'
import json, os, sys
path, kind, version, name, digest = sys.argv[1:]
meta = {
    "kind": kind,
    "version": int(version),
    "versionName": name,
    "file": os.path.basename(path),
    "sha256": digest,
}
out = os.path.splitext(path)[0]
if out.endswith(".tar"):
    out = out[:-4]
out += ".json"
with open(out, "w", encoding="utf-8") as f:
    json.dump(meta, f, ensure_ascii=False, indent=2)
    f.write("\n")
print(out)
PY
}

ensure_env() {
  local dir="$1"
  if [[ ! -f "${dir}/.env" ]]; then
    cp "${dir}/.env.example" "${dir}/.env"
    echo ">> 已从 .env.example 生成 ${dir}/.env（只为构建；生产请改密码）"
  fi
}

run_compose() {
  local dir="$1"
  shift
  (
    cd "$dir"
    # COMPOSE 允许写成 docker compose 或 docker-compose
    # shellcheck disable=SC2086
    set -- ${COMPOSE} "$@"
    "$@"
  )
}

pack_app_image() {
  local kind="$1" side="$2" repo="$3"
  local rel="${ROOT}/${side}/server/internal/platform/release/release.go"
  local web="${ROOT}/${side}/frontend/src/shared/version.ts"
  [[ -f "$rel" ]] || die "找不到 ${rel}"
  local code name build_ver image alt out tmp digest meta
  if [[ "$DO_BUMP" -eq 1 ]]; then
    backup_files "$rel" "$web"
    read -r code name < <(bump_kind "$kind")
    [[ -n "$code" && -n "$name" ]] || die "升版本失败"
    echo "==> ${kind}  v${code}  ${name}（已写入源码）"
  else
    code="$(parse_go_code "$rel")"
    name="$(parse_go_name "$rel")"
    echo "==> ${kind}  v${code}  ${name}（未改版本）"
  fi
  if [[ "$DO_TEST" -eq 1 ]]; then
    echo ">> make test (${side})"
    make -C "${ROOT}/${side}" test
  fi
  need_cmd docker
  ensure_env "${ROOT}/${side}"
  build_ver="$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)"
  echo ">> 构建 ${repo}:${name}（只打 app，不含 db/oss/alloy）"
  (
    cd "${ROOT}/${side}"
    export WMESH_REGISTRY=
    export WMESH_VERSION="$name"
    export WMESH_BUILD_VERSION="$build_ver"
    # shellcheck disable=SC2086
    set -- ${COMPOSE}
    if [[ "$DO_PULL" -eq 1 ]]; then
      "$@" build --pull app
    else
      "$@" build app
    fi
  )
  image="${repo}:${name}"
  alt="${repo}:v${code}"
  docker image inspect "$image" >/dev/null 2>&1 || die "镜像不存在：${image}"
  docker tag "$image" "$alt"
  mkdir -p "$OUT"
  out="${OUT}/${kind}-v${code}-${name}.tar.gz"
  tmp="${out}.tmp"
  echo ">> docker save ${image} ${alt}"
  docker save "$image" "$alt" | gzip -9 >"$tmp"
  mv "$tmp" "$out"
  digest="$(sha256_file "$out")"
  meta="$(write_meta "$out" "$kind" "$code" "$name" "$digest")"
  echo ">> $(ls -lh "$out" | awk '{print $5}')  sha256=${digest}"
  echo ">> 元数据 ${meta}"
  clear_backup
}

pack_apk() {
  local root="${ROOT}/client/ShiJiaoQi-New"
  local gradle="${root}/app/build.gradle.kts"
  [[ -f "${root}/gradlew" && -f "$gradle" ]] || die "找不到 client/ShiJiaoQi-New（独立仓库，见 client/README.md）"
  local code name apk dest out digest meta
  if [[ "$DO_BUMP" -eq 1 ]]; then
    backup_files "$gradle"
    read -r code name < <(bump_kind client_apk)
    [[ -n "$code" && -n "$name" ]] || die "升版本失败"
    echo "==> client_apk  v${code}  ${name}（已写入源码）"
  else
    code="$(parse_gradle_code "$gradle")"
    name="$(parse_gradle_name "$gradle")"
    echo "==> client_apk  v${code}  ${name}（未改版本）"
  fi
  if [[ "$DO_TEST" -eq 1 ]]; then
    echo ">> gradlew check"
    (cd "$root" && ./gradlew --no-daemon check)
  fi
  echo ">> gradlew assembleRelease"
  (cd "$root" && ./gradlew --no-daemon :app:assembleRelease)
  apk="$(find "${root}/app/build/outputs/apk/release" -name '*.apk' ! -name '*unsigned*' 2>/dev/null | head -n1)"
  [[ -n "$apk" && -f "$apk" ]] || die "没有 release APK：${root}/app/build/outputs/apk/release"
  mkdir -p "$OUT"
  dest="${OUT}/client_apk-v${code}-${name}.apk"
  cp "$apk" "$dest"
  digest="$(sha256_file "$dest")"
  meta="$(write_meta "$dest" "client_apk" "$code" "$name" "$digest")"
  echo ">> $(ls -lh "$dest" | awk '{print $5}')  sha256=${digest}"
  echo ">> 元数据 ${meta}"
  clear_backup
}

normalize_kind() {
  case "$1" in
    all) echo all ;;
    wan_service | wan | global) echo wan_service ;;
    factory_service | factory) echo factory_service ;;
    client_apk | client | apk) echo client_apk ;;
    *) die "未知种类：$1（wan_service / factory_service / client_apk / all）" ;;
  esac
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    --out)
      [[ $# -ge 2 ]] || die "--out 需要目录"
      OUT="$2"
      shift 2
      ;;
    --test)
      DO_TEST=1
      shift
      ;;
    --pull)
      DO_PULL=1
      shift
      ;;
    --no-bump)
      DO_BUMP=0
      shift
      ;;
    --)
      shift
      break
      ;;
    -*)
      die "未知选项 $1"
      ;;
    *)
      KINDS+=("$(normalize_kind "$1")")
      shift
      ;;
  esac
done

if [[ ${#KINDS[@]} -eq 0 ]]; then
  KINDS=(all)
fi

SELECTED=()
for k in "${KINDS[@]}"; do
  if [[ "$k" == all ]]; then
    SELECTED=(wan_service factory_service client_apk)
    break
  fi
  SELECTED+=("$k")
done

# 去重且保持 wan → factory → apk 顺序
FINAL=()
for want in wan_service factory_service client_apk; do
  for k in "${SELECTED[@]}"; do
    if [[ "$k" == "$want" ]]; then
      FINAL+=("$want")
      break
    fi
  done
done
[[ ${#FINAL[@]} -gt 0 ]] || die "没有要打的包"

mkdir -p "$OUT"
echo "输出目录：${OUT}"

for k in "${FINAL[@]}"; do
  case "$k" in
    wan_service) pack_app_image wan_service global wmesh-global-app ;;
    factory_service) pack_app_image factory_service factory wmesh-factory-app ;;
    client_apk) pack_apk ;;
  esac
done

echo "==> 完成"
ls -lh "${OUT}"/wan_service-* "${OUT}"/factory_service-* "${OUT}"/client_apk-* 2>/dev/null || true
