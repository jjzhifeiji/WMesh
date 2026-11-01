-- 节点离线授权那套从未接通，代码已删：运行凭证、事实桩、上传记录、下发授权与记录、本机当前使用人一并去掉。
-- +goose Up

DROP TABLE IF EXISTS client_runtime_grants;
DROP TABLE IF EXISTS fact_stubs;
DROP TABLE IF EXISTS upload_records;
DROP TABLE IF EXISTS client_distribution_records;
DROP TABLE IF EXISTS client_distribution_grants;

ALTER TABLE clients DROP COLUMN IF EXISTS operator_id;
