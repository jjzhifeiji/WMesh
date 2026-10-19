-- 内置工程模版只在空库首次落一次：抢到标记才插种子，删光后读取不再补回。
-- +goose Up

CREATE TABLE content_template_seeds (
    kind TEXT PRIMARY KEY, -- 已落过种子的模版类型
    seeded_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 首次落种子时刻
    CHECK (kind IN ('project'))
);

COMMENT ON TABLE content_template_seeds IS '内置模版一次性种子标记：每类一行，有行即不再按代码补种子';
COMMENT ON COLUMN content_template_seeds.kind IS '已落过种子的模版类型；目前只有 project';
COMMENT ON COLUMN content_template_seeds.seeded_at IS '首次落种子时刻；已有库迁移时记迁移时刻';

-- 已有工程模版的库直接记为已初始化，之后删光也不再补回。
INSERT INTO content_template_seeds (kind, seeded_at)
SELECT 'project', now()
WHERE EXISTS (SELECT 1 FROM content_templates WHERE kind = 'project');
