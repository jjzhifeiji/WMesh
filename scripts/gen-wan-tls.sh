#!/usr/bin/env bash
# 给厂↔WAN 链路生成自建 CA 和 WAN 服务端证书。只用 openssl，不连网、不改 .env。
set -euo pipefail

usage() {
  cat <<'EOF'
用法: scripts/gen-wan-tls.sh [--renew] <输出目录> <SAN>...

  <输出目录>  例如 global/tls；生成后的布局：
                ca.key / ca.pem         自建 CA（10 年）；ca.key 生成后移走离线保存
                wan/server.key、wan/server.pem、wan/ca.pem
                                        WAN 服务端证书（825 天）；WMESH_TLS_DIR 指向 wan/
  <SAN>       WAN 对厂可见的地址，IP 或域名都行，可写多个；
              同机部署时要带上 host.docker.internal 或宿主机网关地址
  --renew     沿用已有 CA 只重签服务端证书，厂端的 CA 不用换（需要 ca.key 在输出目录里）

例: scripts/gen-wan-tls.sh global/tls 154.82.81.16 host.docker.internal
EOF
}

die() { echo "gen-wan-tls: $*" >&2; exit 1; }

renew=0
if [[ "${1:-}" == "--renew" ]]; then
  renew=1
  shift
fi
case "${1:-}" in
  -h | --help | "") usage; exit 0 ;;
esac
[[ $# -ge 2 ]] || { usage >&2; exit 2; }
command -v openssl >/dev/null 2>&1 || die "需要命令 openssl"

out="$1"
shift
mkdir -p "$out/wan"
out="$(cd "$out" && pwd)"
umask 077

# 按写法区分 IP 和域名，拼成 subjectAltName。
san=""
for name in "$@"; do
  if [[ "$name" =~ ^[0-9]+(\.[0-9]+){3}$ || "$name" == *:* ]]; then
    entry="IP:$name"
  else
    entry="DNS:$name"
  fi
  san="${san:+$san,}$entry"
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if [[ "$renew" == 1 ]]; then
  [[ -f "$out/ca.key" && -f "$out/ca.pem" ]] || die "--renew 需要 $out/ca.key 和 $out/ca.pem（离线保存的 CA 私钥先临时放回）"
else
  # ca.key 生成后会被移走离线，只看 ca.key 会悄悄换掉各厂已有的 CA，所以 ca.pem 在也拒绝。
  [[ ! -f "$out/ca.key" && ! -f "$out/ca.pem" ]] || die "$out 里已有 CA（ca.pem 或 ca.key）；只换服务端证书用 --renew（把离线的 ca.key 临时放回），重建 CA 先手动移走旧的 ca.pem/ca.key"
  cat > "$tmp/ca.cnf" <<'EOF'
[req]
distinguished_name = dn
prompt = no
[dn]
CN = WMesh WAN CA
[v3_ca]
basicConstraints = critical, CA:TRUE, pathlen:0
keyUsage = critical, keyCertSign, cRLSign
subjectKeyIdentifier = hash
EOF
  openssl ecparam -name prime256v1 -genkey -noout -out "$out/ca.key"
  openssl req -x509 -new -sha256 -days 3650 -key "$out/ca.key" -config "$tmp/ca.cnf" -extensions v3_ca -out "$out/ca.pem"
fi

cat > "$tmp/server.ext" <<EOF
basicConstraints = critical, CA:FALSE
keyUsage = critical, digitalSignature
extendedKeyUsage = serverAuth
subjectAltName = $san
authorityKeyIdentifier = keyid
subjectKeyIdentifier = hash
EOF
# 旧私钥是只读的，先删掉再写新的。
rm -f "$out/wan/server.key" "$out/wan/server.pem"
openssl ecparam -name prime256v1 -genkey -noout -out "$out/wan/server.key"
openssl req -new -sha256 -key "$out/wan/server.key" -subj "/CN=$1" -out "$tmp/server.csr"
openssl x509 -req -sha256 -days 825 -in "$tmp/server.csr" -CA "$out/ca.pem" -CAkey "$out/ca.key" \
  -CAcreateserial -CAserial "$tmp/ca.srl" -extfile "$tmp/server.ext" -out "$out/wan/server.pem"
cp "$out/ca.pem" "$out/wan/ca.pem"
chmod 0644 "$out/ca.pem" "$out/wan/server.pem" "$out/wan/ca.pem"
chmod 0400 "$out/wan/server.key"

# 容器用户是 10001；root 跑就直接交给它，否则提示手动改。
if [[ "$(id -u)" == 0 ]]; then
  chown 10001:10001 "$out/wan/server.key"
  key_hint="已把 server.key 交给容器用户 10001（权限 0400）"
else
  key_hint="容器用户 10001 要读得到私钥：sudo chown 10001:10001 $out/wan/server.key"
fi

cat <<EOF
已生成（SAN: $san，服务端证书 825 天后到期，到期前用 --renew 重签）：
  $out/ca.pem             给各厂，放到厂宿主机绝对路径下，命名为 wan-ca.pem
  $out/wan/server.pem     WAN 服务端证书
  $out/wan/server.key     WAN 服务端私钥
$key_hint
CA 私钥 $out/ca.key 请移走离线保存，只在 --renew 时临时放回。

WAN 的 .env 加上：
  WMESH_TLS_DIR=$out/wan
  WMESH_TLS_CERT_FILE=/etc/wmesh/tls/server.pem
  WMESH_TLS_KEY_FILE=/etc/wmesh/tls/server.key

各厂的 .env（<厂证书目录> 写厂宿主机绝对路径，里面放 wan-ca.pem）：
  WMESH_TLS_DIR=<厂证书目录>
  WMESH_WAN_CA_FILE=/etc/wmesh/tls/wan-ca.pem
  WMESH_WAN_URL=https://$1:52443
  WMESH_WAN_MQTT_URL=ssl://$1:52883
EOF
