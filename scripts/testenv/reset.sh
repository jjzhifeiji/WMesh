#!/usr/bin/env bash
# 把本机测试环境清回空白：删两端数据卷重建、清示教器应用数据、按种子位姿重启假控制器。
# 只用于本机测试数据，不备份、不确认。
#   scripts/testenv/reset.sh          # 沿用本机已有镜像（快）
#   scripts/testenv/reset.sh --build  # 按当前源码重建两端镜像
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CLIENT="$ROOT/client/ShiJiaoQi-New"
FAKE_LOG="$CLIENT/scripts/fake_robot_runs/fake.log"
TARGET=up
[ "${1:-}" = "--build" ] && TARGET=dev-up

say() { printf '\n== %s\n' "$*"; }

say "停假控制器"
pkill -f "scripts/fake_robot.py" 2>/dev/null || true

say "删两端数据卷"
(cd "$ROOT/factory" && docker compose down -v --remove-orphans)
(cd "$ROOT/global" && docker compose down -v --remove-orphans)

say "重建 WAN（make $TARGET）"
make -C "$ROOT/global" "$TARGET"
say "重建厂端（make $TARGET）"
make -C "$ROOT/factory" "$TARGET"

say "等两端健康"
for url in http://localhost:52080/healthz http://localhost:52081/healthz; do
  for _ in $(seq 1 60); do
    curl -fsS -m 3 "$url" >/dev/null 2>&1 && break
    sleep 2
  done
  curl -fsS -m 3 "$url" >/dev/null || { echo "$url 没起来" >&2; exit 1; }
done

say "清示教器应用数据"
if command -v adb >/dev/null && adb get-state >/dev/null 2>&1; then
  adb shell pm clear com.gbndt.shijiaoqi || echo "示教器上没装应用，跳过"
else
  echo "没连示教器，跳过"
fi

say "重启假控制器（种子位姿）"
mkdir -p "$(dirname "$FAKE_LOG")"
rm -f "$CLIENT"/scripts/fake_robot_runs/*.lua
SERIAL="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["fake_robot"]["serial"])' "$ROOT/scripts/testenv/seed.json")"
START="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["fake_robot"]["start_pose_from"])' "$ROOT/scripts/testenv/seed.json")"
nohup python3 "$CLIENT/scripts/fake_robot.py" --serial "$SERIAL" --laser-host 0.0.0.0 \
  --start-from "$ROOT/docs/device-date/projects/$START" >"$FAKE_LOG" 2>&1 &
sleep 2
head -3 "$FAKE_LOG"
"$CLIENT/scripts/sim_net.sh" both

say "重置完成：两端空库，示教器停在登录页，假控制器日志 ${FAKE_LOG#$ROOT/}"
