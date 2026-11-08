#!/usr/bin/env bash
# 把本机测试环境清回空白：起本机 Loki、删两端数据卷重建、清示教器应用数据、按种子位姿重启假控制器。
# 只用于本机测试数据，不备份、不确认。两端 alloy 的 LOKI_URL 覆盖成本机 Loki，日志不推共享环境。
#   scripts/testenv/reset.sh          # 沿用本机已有镜像（快）
#   scripts/testenv/reset.sh --build  # 按当前源码重建两端镜像
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CLIENT="$ROOT/client/ShiJiaoQi-New"
FAKE_LOG="$CLIENT/scripts/fake_robot_runs/fake.log"
TARGET=up
[ "${1:-}" = "--build" ] && TARGET=dev-up

say() { printf '\n== %s\n' "$*"; }

# 两端 alloy 推本机 Loki（容器经 host.docker.internal 访问宿主）。外面已 export 的 LOKI_URL 优先，但只许指向本机，
# 与 logs.py check 同一口径：共享环境或别人机器上的 Loki 一律在动手前拒掉，免得重建、灌数的日志先推出去。
LOKI_COMPOSE="$ROOT/scripts/testenv/loki/docker-compose.yml"
LOKI_READY="http://127.0.0.1:${TESTLOG_LOKI_PORT:-3100}/ready"
export LOKI_URL="${LOKI_URL:-http://host.docker.internal:${TESTLOG_LOKI_PORT:-3100}/loki/api/v1/push}"
case "$LOKI_URL" in
  *154.82.81.16*) echo "LOKI_URL=$LOKI_URL 指向共享环境，本机测试不许推那里；unset LOKI_URL 用本机 Loki" >&2; exit 1 ;;
  http://host.docker.internal[:/]*|http://127.0.0.1[:/]*|http://localhost[:/]*) ;;
  *) echo "LOKI_URL=$LOKI_URL 不是本机 Loki（只认 host.docker.internal / 127.0.0.1 / localhost）；unset LOKI_URL 用本机 Loki" >&2; exit 1 ;;
esac

say "停假控制器"
pkill -f "scripts/fake_robot.py" 2>/dev/null || true

say "起本机 Loki（日志落 scripts/testenv/loki/data/）"
mkdir -p "$ROOT/scripts/testenv/loki/data"
docker compose -f "$LOKI_COMPOSE" up -d
for _ in $(seq 1 60); do
  curl -fsS -m 3 "$LOKI_READY" 2>/dev/null | grep -q ready && break
  sleep 2
done
curl -fsS -m 3 "$LOKI_READY" 2>/dev/null | grep -q ready || { echo "本机 Loki 没起来：$LOKI_READY" >&2; exit 1; }
echo "两端 alloy 推 $LOKI_URL"

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

say "重置完成：两端空库，示教器停在登录页，假控制器日志 ${FAKE_LOG#$ROOT/}，三端日志查 scripts/testenv/logs.py"
