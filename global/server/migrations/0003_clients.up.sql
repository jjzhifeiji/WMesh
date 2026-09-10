-- WAN 登记 Client 公钥与一机一厂绑定；工厂只存签发公钥。审计时间来源可记 local。
-- 不含厂内人员、组织、角色、口令或人员离线授权正文；不含任何私钥。

CREATE TABLE factory_public_keys (
    factory_id UUID PRIMARY KEY REFERENCES factories (id), -- 该厂签发公钥，一对一
    public_key BYTEA NOT NULL, -- Ed25519 公钥 32 字节，无私钥
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 登记时间
    CHECK (octet_length(public_key) = 32)
);

COMMENT ON TABLE factory_public_keys IS '各厂签发用公钥，WAN 只存公开材料';
COMMENT ON COLUMN factory_public_keys.factory_id IS '该厂签发公钥，一对一';
COMMENT ON COLUMN factory_public_keys.public_key IS 'Ed25519 公钥 32 字节，无私钥';
COMMENT ON COLUMN factory_public_keys.created_at IS '登记时间';

CREATE TABLE clients (
    id UUID PRIMARY KEY, -- Client 稳定身份，不由名称生成
    public_key BYTEA NOT NULL, -- 本机公钥，绑定时登记；无私钥
    factory_id UUID REFERENCES factories (id), -- 当前所属工厂；空表示未绑定
    binding_revision BIGINT NOT NULL DEFAULT 0, -- 绑定修订；未绑定为 0，改绑必须升高
    bound_at TIMESTAMPTZ, -- 当前这次绑定生效时间；未绑定为空
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 身份登记时间
    CHECK (octet_length(public_key) = 32),
    CHECK (
        (factory_id IS NULL AND binding_revision = 0 AND bound_at IS NULL)
        OR (factory_id IS NOT NULL AND binding_revision >= 1 AND bound_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX clients_public_key_uq ON clients (public_key); -- 一把公钥只对应一台 Client

COMMENT ON TABLE clients IS '现场节点身份与当前所属工厂；一台同时只属一厂';
COMMENT ON COLUMN clients.id IS 'Client 稳定身份，不由名称生成';
COMMENT ON COLUMN clients.public_key IS '本机公钥，绑定时登记；无私钥';
COMMENT ON COLUMN clients.factory_id IS '当前所属工厂；空表示未绑定';
COMMENT ON COLUMN clients.binding_revision IS '绑定修订；未绑定为 0，改绑必须升高';
COMMENT ON COLUMN clients.bound_at IS '当前这次绑定生效时间；未绑定为空';
COMMENT ON COLUMN clients.created_at IS '身份登记时间';

ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_time_source_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_time_source_check
    CHECK (time_source IN ('server', 'local')); -- 连通 server，断网 local；不改已落库原文

COMMENT ON COLUMN audit_events.time_source IS '时间来源：连通为 server，断网为 local';
