-- 工程模版改为多份独立行：每份自己的名称、字段表、修订。

ALTER TABLE content_templates
    ADD COLUMN name TEXT NOT NULL DEFAULT ''; -- 工程模版名称；工艺为空

ALTER TABLE content_templates
    DROP CONSTRAINT IF EXISTS content_templates_kind_key;

CREATE UNIQUE INDEX content_templates_one_process ON content_templates (kind) WHERE kind = 'process';
CREATE UNIQUE INDEX content_templates_project_name ON content_templates (name) WHERE kind = 'project';

COMMENT ON TABLE content_templates IS '平台当前内容模版：工艺一份，工程多份独立行';
COMMENT ON COLUMN content_templates.id IS '稳定身份';
COMMENT ON COLUMN content_templates.kind IS 'process 工艺 / project 工程';
COMMENT ON COLUMN content_templates.name IS '工程模版名称；工艺为空';
COMMENT ON COLUMN content_templates.revision IS '从 1 起只向前；工程每份自己的修订';
COMMENT ON COLUMN content_templates.schema IS '字段表';
COMMENT ON COLUMN content_templates.digest IS '字段表 SHA-256，32 字节';
COMMENT ON COLUMN content_templates.created_at IS '首次写入';
COMMENT ON COLUMN content_templates.updated_at IS '最近升高修订';
