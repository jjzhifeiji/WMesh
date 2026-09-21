-- 工艺/工程自带作业类型：单层焊道、多层焊缝、T排对接。
-- 已有行先落单层；工程再按正文里的空库模版身份改。

ALTER TABLE assets
    ADD COLUMN weld_kind TEXT NOT NULL DEFAULT 'single'; -- 作业类型

UPDATE assets
SET weld_kind = 'multilayer'
WHERE kind = 'project'
  AND convert_from(content, 'UTF8') LIKE '%22222222-2222-4222-8222-222222222222%';

UPDATE assets
SET weld_kind = 'tbar'
WHERE kind = 'project'
  AND convert_from(content, 'UTF8') LIKE '%44444444-4444-4444-8444-444444444444%';

ALTER TABLE assets
    ADD CONSTRAINT assets_weld_kind_ok CHECK (weld_kind IN ('single', 'multilayer', 'tbar'));

COMMENT ON COLUMN assets.weld_kind IS '作业类型：single 单层焊道 / multilayer 多层焊缝 / tbar T排对接';
