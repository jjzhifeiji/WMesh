-- wan_trust 改存钉住的 WAN 下行指令验签公钥；只改注释，不动表结构和数据。

COMMENT ON TABLE wan_trust IS '本厂钉住的 WAN 下行指令验签公钥；第一次经证书校验的 HTTPS 拉索引时写入，全表一行';
COMMENT ON COLUMN wan_trust.public_key IS 'WAN 指令验签公钥 32 字节；WAN 地址为 https 且已钉时强制验签';
COMMENT ON COLUMN wan_trust.created_at IS '首次钉住时间';
