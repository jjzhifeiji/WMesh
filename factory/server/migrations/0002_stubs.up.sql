-- 归属桩：运行事实与个人资产只记创建时路径，禁止后续改写快照。

CREATE TABLE fact_stubs (
    id UUID PRIMARY KEY, -- 事实桩稳定身份
    creator_id UUID NOT NULL REFERENCES people (id), -- 创建账号稳定身份
    factory_id UUID NOT NULL, -- 所属工厂
    org_unit_id UUID REFERENCES org_units (id), -- 发生节点；直属工厂时为空
    org_path JSONB NOT NULL, -- 当时从工厂到该节点的祖先快照；直属为空
    created_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 发生时间；路径快照此后不得改写
);

CREATE TABLE personal_asset_stubs (
    id UUID PRIMARY KEY, -- 个人资产桩稳定身份
    creator_id UUID NOT NULL REFERENCES people (id), -- 创建人；仅本人可读
    factory_id UUID NOT NULL, -- 所属工厂
    org_unit_id UUID REFERENCES org_units (id), -- 创建时节点；直属时为空
    org_path JSONB NOT NULL, -- 创建时路径，改分配不改写
    content TEXT NOT NULL, -- 内容正文；不进审计
    created_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 创建时间
);
