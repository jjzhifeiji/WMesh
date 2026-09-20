-- 焊接运行事实：每次起停一条，按产生端身份幂等汇聚；不改 fact_stubs。

CREATE TABLE weld_facts (
    id UUID PRIMARY KEY, -- 产生端事实身份
    creator_id UUID NOT NULL REFERENCES people (id), -- 当时登录人
    factory_id UUID NOT NULL, -- 所属工厂
    org_unit_id UUID REFERENCES org_units (id), -- 发生节点；厂直属为空
    org_path JSONB NOT NULL, -- 发生时组织路径快照
    client_id UUID, -- 当时 Client；未匹配可空
    length_mm BIGINT NOT NULL, -- 本段焊长毫米
    duration_sec BIGINT NOT NULL, -- 本段时长秒
    occurred_at TIMESTAMPTZ NOT NULL, -- 焊完时间，按日归集用
    created_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 首次汇聚时间
);

CREATE INDEX weld_facts_creator_occurred ON weld_facts (creator_id, occurred_at);
CREATE INDEX weld_facts_occurred ON weld_facts (occurred_at);

COMMENT ON TABLE weld_facts IS '每次起停一条焊长时长事实，按产生端身份幂等';
COMMENT ON COLUMN weld_facts.id IS '产生端事实身份，汇聚幂等键';
COMMENT ON COLUMN weld_facts.creator_id IS '当时登录人稳定身份';
COMMENT ON COLUMN weld_facts.factory_id IS '所属工厂';
COMMENT ON COLUMN weld_facts.org_unit_id IS '发生节点；厂直属为空';
COMMENT ON COLUMN weld_facts.org_path IS '发生时从工厂到该节点的路径快照';
COMMENT ON COLUMN weld_facts.client_id IS '当时 Client；未匹配可空';
COMMENT ON COLUMN weld_facts.length_mm IS '本段焊长毫米，禁止为负';
COMMENT ON COLUMN weld_facts.duration_sec IS '本段时长秒，禁止为负';
COMMENT ON COLUMN weld_facts.occurred_at IS '焊完时间；日汇总用此时刻';
COMMENT ON COLUMN weld_facts.created_at IS '首次汇聚时间，不覆盖发生时间';

ALTER TABLE weld_facts
    ADD CONSTRAINT weld_facts_length_nonneg CHECK (length_mm >= 0);
ALTER TABLE weld_facts
    ADD CONSTRAINT weld_facts_duration_nonneg CHECK (duration_sec >= 0);
