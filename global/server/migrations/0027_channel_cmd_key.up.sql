-- wan_signing_keys 改作下行通道指令签名钥；只改注释，不动表结构和数据。

COMMENT ON TABLE wan_signing_keys IS '云端下行通道指令签名钥（签名原文前缀 wmesh-wan-cmd-v1），历史上签过软件包；全表一行，私钥不进审计';
COMMENT ON COLUMN wan_signing_keys.public_key IS 'Ed25519 公钥 32 字节；厂端经 HTTPS 索引钉住后用它验下行指令';
COMMENT ON COLUMN wan_signing_keys.private_key IS '云端下行指令签名私钥，启动时载入内存，不进审计';
