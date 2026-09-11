-- 平台级只读副本、工程下发到 Client 的授权与记录、本厂缓存上限。
-- 不改 assets 当前行；Client 缓存正文不进厂库。

CREATE TABLE factory_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1, -- 单行；本厂一份设置
    max_cached_projects INT NOT NULL DEFAULT 2, -- Client 工程缓存上限，按工程份
    CHECK (id = 1),
    CHECK (max_cached_projects >= 1)
);

COMMENT ON TABLE factory_settings IS '本厂级意图：Client 工程缓存上限';
COMMENT ON COLUMN factory_settings.id IS '单行；本厂一份设置';
COMMENT ON COLUMN factory_settings.max_cached_projects IS 'Client 工程缓存上限，按工程份';

INSERT INTO factory_settings (id, max_cached_projects) VALUES (1, 2);

CREATE TABLE asset_replicas (
    id UUID NOT NULL, -- 平台级稳定身份，与 WAN 原件相同
    revision BIGINT NOT NULL, -- 送达时的修订；与身份组成主键
    kind TEXT NOT NULL, -- process / project
    level TEXT NOT NULL, -- 只允许 platform
    name TEXT NOT NULL, -- 显示名，不当身份
    status TEXT NOT NULL, -- 送达时状态：draft / available / disabled
    copyable BOOLEAN NOT NULL, -- 平台级必须为否
    content BYTEA NOT NULL, -- 不透明正文
    digest BYTEA NOT NULL, -- SHA-256 摘要 32 字节
    deps JSONB NOT NULL, -- 工艺必须 []；工程为钉死依赖
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 本厂收到该修订的时间
    PRIMARY KEY (id, revision),
    CHECK (kind IN ('process', 'project')),
    CHECK (level = 'platform'),
    CHECK (copyable = false),
    CHECK (status IN ('draft', 'available', 'disabled')),
    CHECK (revision >= 1),
    CHECK (octet_length(digest) = 32),
    CHECK (
        (kind = 'process' AND deps = '[]'::jsonb)
        OR (kind = 'project' AND jsonb_typeof(deps) = 'array')
    )
);

COMMENT ON TABLE asset_replicas IS '已下发到本厂的平台级只读副本，按身份+修订并存';
COMMENT ON COLUMN asset_replicas.id IS '平台级稳定身份，与 WAN 原件相同';
COMMENT ON COLUMN asset_replicas.revision IS '送达时的修订；与身份组成主键';
COMMENT ON COLUMN asset_replicas.kind IS 'process / project';
COMMENT ON COLUMN asset_replicas.level IS '只允许 platform';
COMMENT ON COLUMN asset_replicas.name IS '显示名，不当身份';
COMMENT ON COLUMN asset_replicas.status IS '送达时状态：draft / available / disabled';
COMMENT ON COLUMN asset_replicas.copyable IS '平台级必须为否';
COMMENT ON COLUMN asset_replicas.content IS '不透明正文';
COMMENT ON COLUMN asset_replicas.digest IS 'SHA-256 摘要 32 字节';
COMMENT ON COLUMN asset_replicas.deps IS '工艺必须 []；工程为钉死依赖';
COMMENT ON COLUMN asset_replicas.received_at IS '本厂收到该修订的时间';

CREATE FUNCTION prevent_replica_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'asset replicas cannot be physically deleted';
END;
$$;

CREATE TRIGGER asset_replicas_no_delete
    BEFORE DELETE ON asset_replicas
    FOR EACH ROW
    EXECUTE FUNCTION prevent_replica_delete();

CREATE TABLE client_distribution_grants (
    id UUID PRIMARY KEY, -- 授权记录稳定身份
    project_id UUID NOT NULL, -- 工程稳定身份；厂级或已收平台级
    client_id UUID NOT NULL REFERENCES clients (id), -- 本厂已绑定 Client
    active BOOLEAN NOT NULL, -- 是否仍有效；收回后不得新下发
    granted_by UUID NOT NULL REFERENCES people (id), -- 授权的本厂超管
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 授权时间
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 最近一次授予或收回
    UNIQUE (project_id, client_id)
);

COMMENT ON TABLE client_distribution_grants IS '工程向本厂 Client 的下发授权；个人级不得授权';
COMMENT ON COLUMN client_distribution_grants.id IS '授权记录稳定身份';
COMMENT ON COLUMN client_distribution_grants.project_id IS '工程稳定身份；厂级或已收平台级';
COMMENT ON COLUMN client_distribution_grants.client_id IS '本厂已绑定 Client';
COMMENT ON COLUMN client_distribution_grants.active IS '是否仍有效；收回后不得新下发';
COMMENT ON COLUMN client_distribution_grants.granted_by IS '授权的本厂超管';
COMMENT ON COLUMN client_distribution_grants.created_at IS '授权时间';
COMMENT ON COLUMN client_distribution_grants.updated_at IS '最近一次授予或收回';

CREATE TABLE client_distribution_records (
    id UUID PRIMARY KEY, -- 下发记录稳定身份
    project_id UUID NOT NULL, -- 工程身份；不存正文
    revision BIGINT NOT NULL, -- 下发时的修订
    client_id UUID NOT NULL REFERENCES clients (id), -- 目标 Client
    closure_digest BYTEA NOT NULL, -- 整包 SHA-256，32 字节
    members JSONB NOT NULL, -- [{id,revision,digest},...]，无正文
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 首次下发时间
    CHECK (revision >= 1),
    CHECK (octet_length(closure_digest) = 32),
    CHECK (jsonb_typeof(members) = 'array'),
    UNIQUE (project_id, revision, client_id)
);

COMMENT ON TABLE client_distribution_records IS '厂内向 Client 下发工程闭包的记录；同一修订幂等，不含正文';
COMMENT ON COLUMN client_distribution_records.id IS '下发记录稳定身份';
COMMENT ON COLUMN client_distribution_records.project_id IS '工程身份；不存正文';
COMMENT ON COLUMN client_distribution_records.revision IS '下发时的修订';
COMMENT ON COLUMN client_distribution_records.client_id IS '目标 Client';
COMMENT ON COLUMN client_distribution_records.closure_digest IS '整包 SHA-256，32 字节';
COMMENT ON COLUMN client_distribution_records.members IS '成员身份+修订+摘要，无正文';
COMMENT ON COLUMN client_distribution_records.created_at IS '首次下发时间';
