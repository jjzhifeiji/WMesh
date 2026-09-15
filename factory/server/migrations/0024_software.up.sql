-- 本厂软件副本与厂服务已装版本；不存包字节，不改 assets。

CREATE TABLE wan_trust (
    id SMALLINT PRIMARY KEY DEFAULT 1, -- 单行
    public_key BYTEA NOT NULL, -- WAN 验签公钥 32 字节
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 写入时间
    CHECK (id = 1),
    CHECK (octet_length(public_key) = 32)
);

COMMENT ON TABLE wan_trust IS '本厂验云端软件包用的 WAN 公钥；全表一行';
COMMENT ON COLUMN wan_trust.id IS '单行';
COMMENT ON COLUMN wan_trust.public_key IS 'WAN 验签公钥 32 字节';
COMMENT ON COLUMN wan_trust.created_at IS '写入时间';

CREATE TABLE software_replicas (
    kind TEXT NOT NULL, -- factory_service / client_apk
    version BIGINT NOT NULL, -- 送达版本
    version_name TEXT NOT NULL, -- 给人看的版本名
    digest BYTEA NOT NULL, -- 包文件 SHA-256，32 字节
    object_key TEXT NOT NULL, -- 本厂对象键
    signature BYTEA NOT NULL, -- WAN 对目标本厂的签名
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 本厂收到时间
    PRIMARY KEY (kind, version),
    CHECK (kind IN ('factory_service', 'client_apk')),
    CHECK (version >= 1),
    CHECK (octet_length(digest) = 32),
    CHECK (octet_length(signature) = 64),
    CHECK (char_length(version_name) BETWEEN 1 AND 80)
);

COMMENT ON TABLE software_replicas IS '已下发到本厂的软件包只读副本元数据；不含字节';
COMMENT ON COLUMN software_replicas.kind IS 'factory_service / client_apk';
COMMENT ON COLUMN software_replicas.version IS '送达版本';
COMMENT ON COLUMN software_replicas.version_name IS '给人看的版本名';
COMMENT ON COLUMN software_replicas.digest IS '包文件 SHA-256，32 字节';
COMMENT ON COLUMN software_replicas.object_key IS '本厂对象键';
COMMENT ON COLUMN software_replicas.signature IS 'WAN 对目标本厂的签名';
COMMENT ON COLUMN software_replicas.received_at IS '本厂收到时间';

CREATE TABLE software_state (
    kind TEXT PRIMARY KEY, -- 目前只记 factory_service
    installed_version BIGINT NOT NULL DEFAULT 0, -- 已确认安装的版本；0 表示未装
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 最近一次确认安装
    CHECK (kind = 'factory_service'),
    CHECK (installed_version >= 0)
);

COMMENT ON TABLE software_state IS '本厂已确认安装的厂服务版本；客户端版本只在本机袋';
COMMENT ON COLUMN software_state.kind IS '目前只记 factory_service';
COMMENT ON COLUMN software_state.installed_version IS '已确认安装的版本；0 表示未装';
COMMENT ON COLUMN software_state.updated_at IS '最近一次确认安装';
