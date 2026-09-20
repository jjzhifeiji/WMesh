-- 厂端上送的焊汇总：只按厂、日、工程名、模式，不含人员与组织。

CREATE TABLE weld_summaries (
    id UUID PRIMARY KEY, -- 汇总行稳定身份
    factory_id UUID NOT NULL REFERENCES factories (id) ON DELETE CASCADE, -- 上送工厂
    day DATE NOT NULL, -- UTC 日历日
    project_name TEXT NOT NULL, -- 当时工程名快照或未关联
    weld_kind TEXT NOT NULL DEFAULT '', -- single / multilayer / tbar / 空
    run_count BIGINT NOT NULL, -- 该粒次数
    length_mm BIGINT NOT NULL, -- 该粒焊长毫米
    duration_sec BIGINT NOT NULL, -- 该粒时长秒
    updated_at TIMESTAMPTZ NOT NULL -- 最近一次被该厂整表替换的时间
);

ALTER TABLE weld_summaries ADD CONSTRAINT weld_summaries_kind_ok
    CHECK (weld_kind IN ('', 'single', 'multilayer', 'tbar')); -- 与厂端焊接模式对齐

ALTER TABLE weld_summaries ADD CONSTRAINT weld_summaries_counts_ok
    CHECK (run_count >= 0 AND length_mm >= 0 AND duration_sec >= 0); -- 不允许负数

ALTER TABLE weld_summaries ADD CONSTRAINT weld_summaries_project_len
    CHECK (char_length(project_name) BETWEEN 1 AND 200); -- 快照名有上限

CREATE UNIQUE INDEX weld_summaries_grain_uq
    ON weld_summaries (factory_id, day, project_name, weld_kind); -- 一厂一日一工程一模式一行

CREATE INDEX weld_summaries_factory_day_idx
    ON weld_summaries (factory_id, day); -- 按厂按日查报表

COMMENT ON TABLE weld_summaries IS '厂端上送的焊长时长汇总，不含人员与组织';
COMMENT ON COLUMN weld_summaries.id IS '汇总行稳定身份';
COMMENT ON COLUMN weld_summaries.factory_id IS '上送该汇总的工厂';
COMMENT ON COLUMN weld_summaries.day IS 'UTC 日历日';
COMMENT ON COLUMN weld_summaries.project_name IS '当时工程显示名快照或未关联';
COMMENT ON COLUMN weld_summaries.weld_kind IS '焊接模式：single / multilayer / tbar / 空';
COMMENT ON COLUMN weld_summaries.run_count IS '该粒起停次数';
COMMENT ON COLUMN weld_summaries.length_mm IS '该粒焊长毫米';
COMMENT ON COLUMN weld_summaries.duration_sec IS '该粒时长秒';
COMMENT ON COLUMN weld_summaries.updated_at IS '最近一次被该厂整表替换的时间';
