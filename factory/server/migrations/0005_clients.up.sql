-- 本厂接受的 Client 绑定、节点运行凭证和人员离线授权；签发密钥只在本厂库。
-- 不存他厂名录、不存 Client 私钥；已落库路径快照与审计原文不改。

CREATE TABLE signing_keys (
    id UUID PRIMARY KEY, -- 本厂签发密钥行身份；全表只能一行
    public_key BYTEA NOT NULL, -- Ed25519 公钥 32 字节
    private_key BYTEA NOT NULL, -- 本厂签发私钥，不是 Client 私钥
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 写入时间
    CHECK (octet_length(public_key) = 32),
    CHECK (octet_length(private_key) = 64)
);

CREATE UNIQUE INDEX signing_keys_one ON signing_keys ((true)); -- 一厂一把签发密钥

COMMENT ON TABLE signing_keys IS '本厂签发密钥；全表一行，不含 Client 私钥';
COMMENT ON COLUMN signing_keys.id IS '本厂签发密钥行身份；全表只能一行';
COMMENT ON COLUMN signing_keys.public_key IS 'Ed25519 公钥 32 字节';
COMMENT ON COLUMN signing_keys.private_key IS '本厂签发私钥，不是 Client 私钥';
COMMENT ON COLUMN signing_keys.created_at IS '写入时间';

CREATE TABLE clients (
    id UUID PRIMARY KEY, -- 与 WAN 相同的 Client 稳定身份
    public_key BYTEA NOT NULL, -- 本机公钥；无私钥
    binding_revision BIGINT NOT NULL, -- 已接受的绑定修订，只向前
    status TEXT NOT NULL, -- bound：可签发；void：已作废不得再签
    bound_at TIMESTAMPTZ NOT NULL, -- 最近一次接受为 bound 的时间
    voided_at TIMESTAMPTZ, -- 作废时间；bound 必须为空
    CHECK (octet_length(public_key) = 32),
    CHECK (binding_revision >= 1),
    CHECK (status IN ('bound', 'void')),
    CHECK (
        (status = 'bound' AND voided_at IS NULL)
        OR (status = 'void' AND voided_at IS NOT NULL)
    )
);

COMMENT ON TABLE clients IS '本厂已接受的 Client 绑定；作废后不得再签发';
COMMENT ON COLUMN clients.id IS '与 WAN 相同的 Client 稳定身份';
COMMENT ON COLUMN clients.public_key IS '本机公钥；无私钥';
COMMENT ON COLUMN clients.binding_revision IS '已接受的绑定修订，只向前';
COMMENT ON COLUMN clients.status IS 'bound：可签发；void：已作废不得再签';
COMMENT ON COLUMN clients.bound_at IS '最近一次接受为 bound 的时间';
COMMENT ON COLUMN clients.voided_at IS '作废时间；bound 必须为空';

CREATE TABLE client_runtime_grants (
    id UUID PRIMARY KEY, -- 节点运行凭证稳定身份
    client_id UUID NOT NULL REFERENCES clients (id), -- 签给本厂这台 Client
    revision BIGINT NOT NULL, -- 该 Client 的节点授权修订，只向前
    can_run BOOLEAN NOT NULL, -- 本修订是否允许运行
    not_before TIMESTAMPTZ NOT NULL, -- 生效时间
    not_after TIMESTAMPTZ NOT NULL, -- 失效时间
    payload BYTEA NOT NULL, -- 被签名的声明原文
    signature BYTEA NOT NULL, -- Ed25519 签名 64 字节
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 写入时间
    CHECK (revision >= 1),
    CHECK (not_after > not_before),
    CHECK (octet_length(payload) > 0),
    CHECK (octet_length(signature) = 64),
    UNIQUE (client_id, revision)
);

COMMENT ON TABLE client_runtime_grants IS '本厂签给已绑定 Client 的节点运行凭证，按修订保留';
COMMENT ON COLUMN client_runtime_grants.id IS '节点运行凭证稳定身份';
COMMENT ON COLUMN client_runtime_grants.client_id IS '签给本厂这台 Client';
COMMENT ON COLUMN client_runtime_grants.revision IS '该 Client 的节点授权修订，只向前';
COMMENT ON COLUMN client_runtime_grants.can_run IS '本修订是否允许运行';
COMMENT ON COLUMN client_runtime_grants.not_before IS '生效时间';
COMMENT ON COLUMN client_runtime_grants.not_after IS '失效时间';
COMMENT ON COLUMN client_runtime_grants.payload IS '被签名的声明原文';
COMMENT ON COLUMN client_runtime_grants.signature IS 'Ed25519 签名 64 字节';
COMMENT ON COLUMN client_runtime_grants.created_at IS '写入时间';

CREATE TABLE person_offline_grants (
    id UUID PRIMARY KEY, -- 人员离线授权稳定身份
    person_id UUID NOT NULL REFERENCES people (id), -- 本厂账号稳定身份
    client_id UUID NOT NULL REFERENCES clients (id), -- 绑定到的本厂 Client
    revision BIGINT NOT NULL, -- 该账号在该 Client 上的授权修订，只向前
    login_name TEXT NOT NULL, -- 签发时登录名，离线对照用，不是身份
    password_hash TEXT NOT NULL, -- 该人口令验证材料副本，不是全厂账号库
    allow_direct BOOLEAN NOT NULL, -- 是否允许 Factory 直属
    org_snapshot JSONB NOT NULL, -- 当时可选 OrgUnit 及祖先路径
    roles_snapshot JSONB NOT NULL, -- 当时固定角色与作用域
    not_before TIMESTAMPTZ NOT NULL, -- 生效时间
    not_after TIMESTAMPTZ NOT NULL, -- 失效时间
    payload BYTEA NOT NULL, -- 被签名的声明原文
    signature BYTEA NOT NULL, -- Ed25519 签名 64 字节
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 写入时间
    CHECK (revision >= 1),
    CHECK (not_after > not_before),
    CHECK (octet_length(payload) > 0),
    CHECK (octet_length(signature) = 64),
    UNIQUE (person_id, client_id, revision)
);

COMMENT ON TABLE person_offline_grants IS '签给本厂账号、绑定特定 Client 的人员离线授权快照';
COMMENT ON COLUMN person_offline_grants.id IS '人员离线授权稳定身份';
COMMENT ON COLUMN person_offline_grants.person_id IS '本厂账号稳定身份';
COMMENT ON COLUMN person_offline_grants.client_id IS '绑定到的本厂 Client';
COMMENT ON COLUMN person_offline_grants.revision IS '该账号在该 Client 上的授权修订，只向前';
COMMENT ON COLUMN person_offline_grants.login_name IS '签发时登录名，离线对照用，不是身份';
COMMENT ON COLUMN person_offline_grants.password_hash IS '该人口令验证材料副本，不是全厂账号库';
COMMENT ON COLUMN person_offline_grants.allow_direct IS '是否允许 Factory 直属';
COMMENT ON COLUMN person_offline_grants.org_snapshot IS '当时可选 OrgUnit 及祖先路径';
COMMENT ON COLUMN person_offline_grants.roles_snapshot IS '当时固定角色与作用域';
COMMENT ON COLUMN person_offline_grants.not_before IS '生效时间';
COMMENT ON COLUMN person_offline_grants.not_after IS '失效时间';
COMMENT ON COLUMN person_offline_grants.payload IS '被签名的声明原文';
COMMENT ON COLUMN person_offline_grants.signature IS 'Ed25519 签名 64 字节';
COMMENT ON COLUMN person_offline_grants.created_at IS '写入时间';

ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_time_source_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_time_source_check
    CHECK (time_source IN ('server', 'local')); -- 连通 server，断网 local；不改已落库原文

COMMENT ON COLUMN audit_events.time_source IS '时间来源：连通为 server，断网为 local';
