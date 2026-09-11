-- 本厂厂级与个人级工艺/工程；正文进库，不进 OSS。
-- 不改个人资产桩、路径快照或审计原文；不存平台级副本。

CREATE TABLE assets (
    id UUID PRIMARY KEY, -- 稳定身份，不由显示名生成
    kind TEXT NOT NULL, -- process 工艺 / project 工程
    level TEXT NOT NULL, -- factory 厂级 / personal 个人级
    name TEXT NOT NULL, -- 显示名，不当身份
    status TEXT NOT NULL, -- draft / available / disabled
    copyable BOOLEAN NOT NULL, -- 可否被上一级复制升档
    revision BIGINT NOT NULL, -- 当前修订，从 1 起只向前
    content BYTEA NOT NULL, -- 不透明正文
    digest BYTEA NOT NULL, -- SHA-256 摘要 32 字节
    creator_id UUID NOT NULL REFERENCES people (id), -- 创建人；个人级仅本人可读正文
    factory_id UUID NOT NULL, -- 所属本厂；不引用 WAN 名录
    org_unit_id UUID REFERENCES org_units (id), -- 创建时节点；直属为空
    org_path JSONB NOT NULL, -- 创建时路径快照，之后不改写
    source_id UUID, -- 升档源身份；非升档为空
    source_revision BIGINT, -- 升档源修订；与 source_id 同空或同有
    deps JSONB NOT NULL, -- 工艺必须 []；工程为 [{id,revision,digest},...]
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 创建时间
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 最近一次升高修订的时间
    CHECK (kind IN ('process', 'project')),
    CHECK (level IN ('factory', 'personal')),
    CHECK (status IN ('draft', 'available', 'disabled')),
    CHECK (revision >= 1),
    CHECK (octet_length(digest) = 32),
    CHECK (
        (kind = 'process' AND deps = '[]'::jsonb)
        OR (kind = 'project' AND jsonb_typeof(deps) = 'array')
    ),
    CHECK (
        (source_id IS NULL AND source_revision IS NULL)
        OR (source_id IS NOT NULL AND source_revision >= 1)
    )
);

COMMENT ON TABLE assets IS '本厂厂级与个人级工艺/工程当前行，不保留历史正文';
COMMENT ON COLUMN assets.id IS '稳定身份，不由显示名生成';
COMMENT ON COLUMN assets.kind IS 'process 工艺 / project 工程';
COMMENT ON COLUMN assets.level IS 'factory 厂级 / personal 个人级';
COMMENT ON COLUMN assets.name IS '显示名，不当身份';
COMMENT ON COLUMN assets.status IS 'draft / available / disabled';
COMMENT ON COLUMN assets.copyable IS '可否被上一级复制升档';
COMMENT ON COLUMN assets.revision IS '当前修订，从 1 起只向前';
COMMENT ON COLUMN assets.content IS '不透明正文';
COMMENT ON COLUMN assets.digest IS 'SHA-256 摘要 32 字节';
COMMENT ON COLUMN assets.creator_id IS '创建人；个人级仅本人可读正文';
COMMENT ON COLUMN assets.factory_id IS '所属本厂；不引用 WAN 名录';
COMMENT ON COLUMN assets.org_unit_id IS '创建时节点；直属为空';
COMMENT ON COLUMN assets.org_path IS '创建时路径快照，之后不改写';
COMMENT ON COLUMN assets.source_id IS '升档源身份；非升档为空';
COMMENT ON COLUMN assets.source_revision IS '升档源修订；与 source_id 同空或同有';
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
