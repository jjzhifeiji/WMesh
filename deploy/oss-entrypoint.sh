#!/bin/sh
set -eu
: "${MINIO_ROOT_USER:?}"
: "${MINIO_ROOT_PASSWORD:?}"
bucket="${WMESH_OSS_BUCKET:-wmesh}"

minio server /data --console-address ":9001" &
pid=$!

export MC_HOST_local="http://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@127.0.0.1:9000"
i=0
while [ "$i" -lt 30 ]; do
	if mc ready local >/dev/null 2>&1; then
		mc mb -p "local/${bucket}" >/dev/null 2>&1 || true
		break
	fi
	i=$((i + 1))
	sleep 1
done

wait "$pid"
