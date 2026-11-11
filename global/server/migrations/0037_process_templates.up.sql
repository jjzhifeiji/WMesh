-- 工艺模版从全库一份改成多份独立行：每份自己的名称、字段表、修订，和工程模版一样可新建、删除。
-- 原来那一份改成预置的「焊接参数」身份；工艺记下自己属于哪份模版，列表和工程槽按它区分种类。
-- +goose Up

DROP INDEX IF EXISTS content_templates_one_process;

-- 原来唯一的工艺模版换成预置「焊接参数」的固定身份和名称，没写所属模版的工艺正文按这个身份归类。
UPDATE content_templates
SET id = '55555555-5555-4555-8555-555555555555', name = '焊接参数'
WHERE kind = 'process';

-- 同类模版名称不重复。
CREATE UNIQUE INDEX content_templates_process_name ON content_templates (name) WHERE kind = 'process';

COMMENT ON TABLE content_templates IS '平台当前内容模版：工艺、工程都是多份独立行';
COMMENT ON COLUMN content_templates.name IS '模版名称；同类不重复';
COMMENT ON COLUMN content_templates.revision IS '从 1 起只向前；每份自己的修订';

-- 工艺模版也走一次性种子：空库落预置两份，删掉后不补回。
ALTER TABLE content_template_seeds
    DROP CONSTRAINT IF EXISTS content_template_seeds_kind_check;
ALTER TABLE content_template_seeds
    ADD CONSTRAINT content_template_seeds_kind_check CHECK (kind IN ('process', 'project'));

COMMENT ON COLUMN content_template_seeds.kind IS '已落过种子的模版类型：process / project';

COMMENT ON TABLE content_template_tombstones IS '已删模版的修订墓碑：按原身份补建时修订从这里往上接，工艺、工程共用';

ALTER TABLE assets
    ADD COLUMN template_id TEXT NOT NULL DEFAULT ''; -- 工艺所属模版身份

-- 已有工艺按包角标记归到预置两份。
UPDATE assets
SET template_id = CASE WHEN corner THEN '66666666-6666-4666-8666-666666666666' ELSE '55555555-5555-4555-8555-555555555555' END
WHERE kind = 'process';

COMMENT ON COLUMN assets.template_id IS '工艺所属模版身份，随正文算：正文写了 templateId 用它，没写的包角归预置包角、其余归预置焊接参数；工程为空';
