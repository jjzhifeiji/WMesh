-- 本厂原件和平台级副本都记下作业类型；已有行先单层。厂库正文是信封，不按 JSON 回填。

ALTER TABLE assets
    ADD COLUMN weld_kind TEXT NOT NULL DEFAULT 'single'; -- 作业类型

ALTER TABLE assets
    ADD CONSTRAINT assets_weld_kind_ok CHECK (weld_kind IN ('single', 'multilayer', 'tbar'));

COMMENT ON COLUMN assets.weld_kind IS '作业类型：single 单层焊道 / multilayer 多层焊缝 / tbar T排对接';

ALTER TABLE asset_replicas
    ADD COLUMN weld_kind TEXT NOT NULL DEFAULT 'single'; -- 与源相同

ALTER TABLE asset_replicas
    ADD CONSTRAINT asset_replicas_weld_kind_ok CHECK (weld_kind IN ('single', 'multilayer', 'tbar'));

COMMENT ON COLUMN asset_replicas.weld_kind IS '作业类型：与源相同；single / multilayer / tbar';
