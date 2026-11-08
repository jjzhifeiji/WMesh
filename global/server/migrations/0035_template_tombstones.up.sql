-- 删除工程模版时记下身份和最后修订；按原身份补建时修订接着往上走，厂端不会把撤下的旧副本当成补建的这一份。
-- +goose Up

CREATE TABLE content_template_tombstones (
    id UUID PRIMARY KEY, -- 已删除工程模版的身份
    revision BIGINT NOT NULL CHECK (revision >= 1), -- 删除前用到的最高修订
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 最近一次删除时刻
);

COMMENT ON TABLE content_template_tombstones IS '已删除工程模版的修订墓碑；按原身份补建时修订从这里往上接，不存字段表';
COMMENT ON COLUMN content_template_tombstones.id IS '已删除工程模版的身份';
COMMENT ON COLUMN content_template_tombstones.revision IS '删除前用到的最高修订；再删时取较大值';
COMMENT ON COLUMN content_template_tombstones.deleted_at IS '最近一次删除时刻';
