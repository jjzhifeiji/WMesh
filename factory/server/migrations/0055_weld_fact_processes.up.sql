-- 焊次补工艺追溯（D-109）：开焊时的工程修订和这次用到的工艺清单。旧行两列都空，不回填。
-- +goose Up

ALTER TABLE weld_facts
    ADD COLUMN project_revision BIGINT, -- 开焊时示教器本机的工程修订；旧行和未打开工程为空
    ADD COLUMN processes JSONB; -- 这次用到的工艺 [{processId, revision, digest}]；旧行和没记到的为空

COMMENT ON COLUMN weld_facts.project_revision IS '开焊时示教器本机的工程修订；本机改过没推时可能落后；旧行为空';
COMMENT ON COLUMN weld_facts.processes IS '这次用到的工艺清单，每条是身份、本机修订和正文摘要（标准 Base64 的 SHA-256），不含参数值；旧行为空';

ALTER TABLE weld_facts
    ADD CONSTRAINT weld_facts_project_revision_nonneg CHECK (project_revision IS NULL OR project_revision >= 0);
