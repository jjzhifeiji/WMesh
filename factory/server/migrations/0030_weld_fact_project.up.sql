-- 焊事实补工程快照与焊接模式，供按工程汇总和下钻每次。

ALTER TABLE weld_facts
    ADD COLUMN project_id UUID, -- 当时工程身份；未打开可空
    ADD COLUMN project_name TEXT NOT NULL DEFAULT '', -- 当时工程名快照，改名不回写
    ADD COLUMN weld_kind TEXT NOT NULL DEFAULT ''; -- single / multilayer / tbar / 空

CREATE INDEX weld_facts_project_occurred ON weld_facts (project_id, occurred_at);

COMMENT ON COLUMN weld_facts.project_id IS '当时工程稳定身份；未打开工程可空';
COMMENT ON COLUMN weld_facts.project_name IS '当时工程显示名快照，改名不改历史';
COMMENT ON COLUMN weld_facts.weld_kind IS '焊接模式：single / multilayer / tbar / 空';

ALTER TABLE weld_facts
    ADD CONSTRAINT weld_facts_kind_ok CHECK (weld_kind IN ('', 'single', 'multilayer', 'tbar'));
