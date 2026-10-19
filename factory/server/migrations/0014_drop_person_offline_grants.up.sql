-- 不再由厂里签发人员离线授权；本厂有效账号默认可用本厂设备。
-- +goose Up

DROP TABLE IF EXISTS person_offline_grants;
