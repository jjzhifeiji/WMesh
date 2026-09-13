-- 每厂一把内容租约钥，只用于续发和解过站信封；不进审计、不存厂正文。

CREATE TABLE content_leases (
    factory_id UUID PRIMARY KEY REFERENCES factories (id), -- 这家厂
    lease_key BYTEA NOT NULL, -- 32 字节租约钥 L
    not_after TIMESTAMPTZ NOT NULL, -- WAN 钟上的到期时间
    renewed_at TIMESTAMPTZ NOT NULL -- 最近一次签发或续期
);

COMMENT ON TABLE content_leases IS 'WAN 为各厂保留的当前内容租约钥，只用于续发';
COMMENT ON COLUMN content_leases.factory_id IS '这家厂';
COMMENT ON COLUMN content_leases.lease_key IS '32 字节租约钥 L，不进审计';
COMMENT ON COLUMN content_leases.not_after IS 'WAN 钟上的到期时间，一次最多 24 小时';
COMMENT ON COLUMN content_leases.renewed_at IS '最近一次签发或续期';
