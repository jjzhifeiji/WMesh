# pack-ca

`scripts/pack.sh` 打厂端包时，把 WAN 的 CA 公钥临时拷成这里的 `wan-ca.pem`，打进镜像 `/etc/wmesh/pack-ca/`，打完就删。

厂端启动时发现镜像里有这份 CA、`.env` 又没另配 `WMESH_WAN_CA_FILE`，就用它校验 WAN，并把明文的 WAN 地址改成同主机的 TLS 端口（HTTPS 52443、MQTTS 52883，可用 `WMESH_WAN_HTTPS_PORT`、`WMESH_WAN_MQTTS_PORT` 改）。WAN 还没开 TLS 时，在厂端 `.env` 写 `WMESH_WAN_TLS=off` 退回明文。

`wan-ca.pem` 不入库。
