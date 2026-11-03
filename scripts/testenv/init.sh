#!/usr/bin/env bash
# 一键把本机测试环境重置并灌好初始数据，核对后写基线。
#   scripts/testenv/init.sh                  # 重置 + 基础数据
#   scripts/testenv/init.sh --with-history   # 再加过去 30 天的焊次
#   scripts/testenv/init.sh --build          # 两端按当前源码重建镜像
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
RESET_ARGS=()
SEED_ARGS=()
for a in "$@"; do
  case "$a" in
    --build) RESET_ARGS+=("--build") ;;
    --with-history) SEED_ARGS+=("--with-history") ;;
    *) echo "不认识的参数：$a" >&2; exit 2 ;;
  esac
done

"$HERE/reset.sh" "${RESET_ARGS[@]+"${RESET_ARGS[@]}"}"
python3 "$HERE/seed.py" "${SEED_ARGS[@]+"${SEED_ARGS[@]}"}"
python3 "$HERE/verify.py"
