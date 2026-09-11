-- 平台级资产下发授权与记录；不存闭包正文、厂内人员或厂级原件。

CREATE TABLE distribution_grants (
    id UUID PRIMARY KEY, -- 授权记录稳定身份
    asset_id UUID NOT NULL REFERENCES assets (id), -- 被授权接收的平台级资产
    factory_id UUID NOT NULL REFERENCES factories (id), -- 获准接收的工厂
    active BOOLEAN NOT NULL, -- 是否仍有效；收回后不得新下发
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 授权时间
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 最近一次授予或收回
    UNIQUE (asset_id, factory_id)
);

COMMENT ON TABLE distribution_grants IS '平台级资产向工厂的下发授权；收回后不得新下发';
COMMENT ON COLUMN distribution_grants.id IS '授权记录稳定身份';
COMMENT ON COLUMN distribution_grants.asset_id IS '被授权接收的平台级资产';
COMMENT ON COLUMN distribution_grants.factory_id IS '获准接收的工厂';
COMMENT ON COLUMN distribution_grants.active IS '是否仍有效；收回后不得新下发';
COMMENT ON COLUMN distribution_grants.created_at IS '授权时间';
COMMENT ON COLUMN distribution_grants.updated_at IS '最近一次授予或收回';

CREATE TABLE distribution_records (
    id UUID PRIMARY KEY, -- 下发记录稳定身份
    asset_id UUID NOT NULL, -- 根资产身份；不存正文
    revision BIGINT NOT NULL, -- 下发时的修订
    factory_id UUID NOT NULL REFERENCES factories (id), -- 目标工厂
    kind TEXT NOT NULL, -- process / project
    closure_digest BYTEA NOT NULL, -- 整包 SHA-256，32 字节
    members JSONB NOT NULL, -- [{id,revision,digest},...]，无正文
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 首次下发时间
    CHECK (kind IN ('process', 'project')),
    CHECK (revision >= 1),
    CHECK (octet_length(closure_digest) = 32),
    CHECK (jsonb_typeof(members) = 'array'),
    UNIQUE (asset_id, revision, factory_id)
);

COMMENT ON TABLE distribution_records IS 'WAN 向工厂下发闭包的记录；同一修订幂等，不含正文';
COMMENT ON COLUMN distribution_records.id IS '下发记录稳定身份';
COMMENT ON COLUMN distribution_records.asset_id IS '根资产身份；不存正文';
COMMENT ON COLUMN distribution_records.revision IS '下发时的修订';
COMMENT ON COLUMN distribution_records.factory_id IS '目标工厂';
COMMENT ON COLUMN distribution_records.kind IS 'process / project';
COMMENT ON COLUMN distribution_records.closure_digest IS '整包 SHA-256，32 字节';
COMMENT ON COLUMN distribution_records.members IS '成员身份+修订+摘要，无正文';
COMMENT ON COLUMN distribution_records.created_at IS '首次下发时间';
