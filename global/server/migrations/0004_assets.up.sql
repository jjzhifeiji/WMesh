-- WAN 平台级工艺/工程；正文进库，不进 OSS。
-- 不存厂级/个人级原件；升档来源身份只做溯源。

CREATE TABLE assets (
    id UUID PRIMARY KEY, -- 稳定身份，不由显示名生成
    kind TEXT NOT NULL, -- process 工艺 / project 工程
    level TEXT NOT NULL, -- 只允许 platform
    name TEXT NOT NULL, -- 显示名，不当身份
    status TEXT NOT NULL, -- draft / available / disabled
    copyable BOOLEAN NOT NULL, -- 平台级必须为否
    revision BIGINT NOT NULL, -- 当前修订，从 1 起只向前
    content BYTEA NOT NULL, -- 不透明正文
    digest BYTEA NOT NULL, -- SHA-256 摘要 32 字节
    creator_id UUID NOT NULL REFERENCES wan_admins (id), -- 创建人，WAN 管理员
    source_id UUID, -- 升档源厂级身份；WAN 制作则为空
    source_revision BIGINT, -- 升档源修订
    source_factory_id UUID REFERENCES factories (id), -- 升档源厂；与 source_id 同空或同有
    deps JSONB NOT NULL, -- 工艺必须 []；工程为 [{id,revision,digest},...]
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 创建时间
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 最近一次升高修订的时间
    CHECK (kind IN ('process', 'project')),
    CHECK (level = 'platform'),
    CHECK (copyable = false),
    CHECK (status IN ('draft', 'available', 'disabled')),
    CHECK (revision >= 1),
    CHECK (octet_length(digest) = 32),
    CHECK (
        (kind = 'process' AND deps = '[]'::jsonb)
        OR (kind = 'project' AND jsonb_typeof(deps) = 'array')
    ),
    CHECK (
        (source_id IS NULL AND source_revision IS NULL AND source_factory_id IS NULL)
        OR (source_id IS NOT NULL AND source_revision >= 1 AND source_factory_id IS NOT NULL)
    )
);

COMMENT ON TABLE assets IS '平台级工艺/工程当前行；不持厂内原件';
COMMENT ON COLUMN assets.id IS '稳定身份，不由显示名生成';
COMMENT ON COLUMN assets.kind IS 'process 工艺 / project 工程';
COMMENT ON COLUMN assets.level IS '只允许 platform';
COMMENT ON COLUMN assets.name IS '显示名，不当身份';
COMMENT ON COLUMN assets.status IS 'draft / available / disabled';
COMMENT ON COLUMN assets.copyable IS '平台级必须为否';
COMMENT ON COLUMN assets.revision IS '当前修订，从 1 起只向前';
COMMENT ON COLUMN assets.content IS '不透明正文';
COMMENT ON COLUMN assets.digest IS 'SHA-256 摘要 32 字节';
COMMENT ON COLUMN assets.creator_id IS '创建人，WAN 管理员';
COMMENT ON COLUMN assets.source_id IS '升档源厂级身份；WAN 制作则为空';
COMMENT ON COLUMN assets.source_revision IS '升档源修订';
COMMENT ON COLUMN assets.source_factory_id IS '升档源厂；与 source_id 同空或同有';
COMMENT ON COLUMN assets.deps IS '工艺必须 []；工程为 [{id,revision,digest},...]';
COMMENT ON COLUMN assets.created_at IS '创建时间';
COMMENT ON COLUMN assets.updated_at IS '最近一次升高修订的时间';

CREATE FUNCTION prevent_asset_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'assets cannot be physically deleted';
END;
$$;

CREATE TRIGGER assets_no_delete
    BEFORE DELETE ON assets
    FOR EACH ROW
    EXECUTE FUNCTION prevent_asset_delete();
