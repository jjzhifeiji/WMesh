-- 焊事实的发生节点改按快照存：去掉 org_unit_id 外键，和 project_id 一样不随节点存废变化。
-- 示教器离线期间节点被删，回连上送的旧事实不再因外键被拒、一直重试。已到库的事实仍由 org_path 挡删。
-- +goose Up

ALTER TABLE weld_facts DROP CONSTRAINT IF EXISTS weld_facts_org_unit_id_fkey;

COMMENT ON COLUMN weld_facts.org_unit_id IS '发生节点身份快照；厂直属为空。节点删了也保留，不设外键';
