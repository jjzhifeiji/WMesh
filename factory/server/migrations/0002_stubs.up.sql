-- 归属桩：运行事实与个人资产只记创建时路径，禁止后续改写快照。

CREATE TABLE fact_stubs (
    id UUID PRIMARY KEY,
    creator_id UUID NOT NULL REFERENCES people (id),
    factory_id UUID NOT NULL,
    org_unit_id UUID REFERENCES org_units (id),
    org_path JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE personal_asset_stubs (
    id UUID PRIMARY KEY,
    creator_id UUID NOT NULL REFERENCES people (id),
    factory_id UUID NOT NULL,
    org_unit_id UUID REFERENCES org_units (id),
    org_path JSONB NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
