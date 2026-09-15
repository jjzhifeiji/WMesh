-- 软件发布与按下发记录；不存包字节、厂内人员或工艺正文。

CREATE TABLE wan_signing_keys (
    id UUID PRIMARY KEY, -- 云端签发密钥行身份；全表只能一行
    public_key BYTEA NOT NULL, -- Ed25519 公钥 32 字节
    private_key BYTEA NOT NULL, -- 云端签发私钥，不进审计
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 写入时间
    CHECK (octet_length(public_key) = 32),
    CHECK (octet_length(private_key) = 64)
);

CREATE UNIQUE INDEX wan_signing_keys_one ON wan_signing_keys ((true)); -- 全云端一把签发密钥

COMMENT ON TABLE wan_signing_keys IS '云端签发软件包的密钥；全表一行，私钥不进审计';
COMMENT ON COLUMN wan_signing_keys.id IS '云端签发密钥行身份；全表只能一行';
COMMENT ON COLUMN wan_signing_keys.public_key IS 'Ed25519 公钥 32 字节';
COMMENT ON COLUMN wan_signing_keys.private_key IS '云端签发私钥，不进审计';
COMMENT ON COLUMN wan_signing_keys.created_at IS '写入时间';

CREATE TABLE software_releases (
    kind TEXT NOT NULL, -- factory_service / client_apk
    version BIGINT NOT NULL, -- 单调整数，只向前比较
    version_name TEXT NOT NULL, -- 给人看的版本名，不当身份
    digest BYTEA NOT NULL, -- 包文件 SHA-256，32 字节
    object_key TEXT NOT NULL, -- 对象存储键；字节不进本表
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 首次发布时间
    PRIMARY KEY (kind, version),
    CHECK (kind IN ('factory_service', 'client_apk')),
    CHECK (version >= 1),
    CHECK (octet_length(digest) = 32),
    CHECK (char_length(version_name) BETWEEN 1 AND 80)
);

COMMENT ON TABLE software_releases IS '云端软件发布原件元数据；不含包字节';
COMMENT ON COLUMN software_releases.kind IS 'factory_service / client_apk';
COMMENT ON COLUMN software_releases.version IS '单调整数，只向前比较';
COMMENT ON COLUMN software_releases.version_name IS '给人看的版本名，不当身份';
COMMENT ON COLUMN software_releases.digest IS '包文件 SHA-256，32 字节';
COMMENT ON COLUMN software_releases.object_key IS '对象存储键；字节不进本表';
COMMENT ON COLUMN software_releases.created_at IS '首次发布时间';

CREATE TABLE software_distributions (
    kind TEXT NOT NULL, -- 与发布种类相同
    version BIGINT NOT NULL, -- 下发的版本
    factory_id UUID NOT NULL REFERENCES factories (id), -- 目标已认领工厂
    signature BYTEA NOT NULL, -- WAN 对种类+版本+摘要+目标厂的签名
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 首次下发时间
    PRIMARY KEY (kind, version, factory_id),
    CHECK (kind IN ('factory_service', 'client_apk')),
    CHECK (version >= 1),
    CHECK (octet_length(signature) = 64),
    FOREIGN KEY (kind, version) REFERENCES software_releases (kind, version)
);

COMMENT ON TABLE software_distributions IS 'WAN 向某厂下发过的软件版本；同一份幂等';
COMMENT ON COLUMN software_distributions.kind IS '与发布种类相同';
COMMENT ON COLUMN software_distributions.version IS '下发的版本';
COMMENT ON COLUMN software_distributions.factory_id IS '目标已认领工厂';
COMMENT ON COLUMN software_distributions.signature IS 'WAN 对种类+版本+摘要+目标厂的签名';
COMMENT ON COLUMN software_distributions.created_at IS '首次下发时间';
