-- 记下已删除的平台级身份，厂回连时补送撤回。

CREATE TABLE asset_retractions (
    asset_id UUID PRIMARY KEY, -- 已删除的平台级身份
    created_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 删除时间
);

COMMENT ON TABLE asset_retractions IS '已删除的平台级身份；厂回连补送撤回，不存正文';
COMMENT ON COLUMN asset_retractions.asset_id IS '已删除的平台级身份';
COMMENT ON COLUMN asset_retractions.created_at IS '删除时间';
