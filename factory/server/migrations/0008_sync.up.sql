-- 上传记录：只记元数据，正文在对象存根，不进审计、不进 WAN。
-- 不改 fact_stubs；汇聚沿用其主键做产生端幂等键。

CREATE TABLE upload_records (
    id UUID PRIMARY KEY, -- 产生端稳定身份，汇聚幂等键
    kind TEXT NOT NULL, -- point_cloud / image
    object_key TEXT NOT NULL, -- 对象存储键，不含正文
    digest BYTEA NOT NULL, -- SHA-256 摘要 32 字节
    byte_size BIGINT NOT NULL, -- 正文字节数
    creator_id UUID NOT NULL REFERENCES people (id), -- 上传人稳定身份
    client_id UUID NOT NULL REFERENCES clients (id), -- 来源 Client
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 首次汇聚时间；不按本机钟覆盖
    CHECK (kind IN ('point_cloud', 'image')),
    CHECK (octet_length(digest) = 32),
    CHECK (byte_size >= 0)
);

COMMENT ON TABLE upload_records IS 'Client 主动上传点云/图片的记录，不含正文';
COMMENT ON COLUMN upload_records.id IS '产生端稳定身份，汇聚幂等键';
COMMENT ON COLUMN upload_records.kind IS 'point_cloud / image';
COMMENT ON COLUMN upload_records.object_key IS '对象存储键，不含正文';
COMMENT ON COLUMN upload_records.digest IS 'SHA-256 摘要 32 字节';
COMMENT ON COLUMN upload_records.byte_size IS '正文字节数';
COMMENT ON COLUMN upload_records.creator_id IS '上传人稳定身份';
COMMENT ON COLUMN upload_records.client_id IS '来源 Client';
COMMENT ON COLUMN upload_records.created_at IS '首次汇聚时间；不按本机钟覆盖';
