-- 工艺/工程各一份当前字段模版；不是焊接资产，不进 assets。

CREATE TABLE content_templates (
    id UUID PRIMARY KEY, -- 稳定身份，每类一个
    kind TEXT NOT NULL, -- process / project
    revision BIGINT NOT NULL, -- 从 1 起只向前
    schema JSONB NOT NULL, -- 字段表
    digest BYTEA NOT NULL, -- 字段表 SHA-256，32 字节
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 首次写入
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 最近升高修订
    UNIQUE (kind),
    CHECK (kind IN ('process', 'project')),
    CHECK (revision >= 1),
    CHECK (octet_length(digest) = 32),
    CHECK (jsonb_typeof(schema) = 'object')
);

COMMENT ON TABLE content_templates IS '平台当前内容模版：工艺一份、工程一份';
COMMENT ON COLUMN content_templates.id IS '稳定身份，每类一个';
COMMENT ON COLUMN content_templates.kind IS 'process 工艺 / project 工程';
COMMENT ON COLUMN content_templates.revision IS '从 1 起只向前';
COMMENT ON COLUMN content_templates.schema IS '字段表';
COMMENT ON COLUMN content_templates.digest IS '字段表 SHA-256，32 字节';
COMMENT ON COLUMN content_templates.created_at IS '首次写入';
COMMENT ON COLUMN content_templates.updated_at IS '最近升高修订';
