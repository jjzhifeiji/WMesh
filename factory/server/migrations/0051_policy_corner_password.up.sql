-- 本厂策略加 T 排/包角口令：示教器进 T 排、解包角几何锁时核对；随登录和对账清单的策略下发，不进 MQTT 签名小信封。
-- +goose Up

ALTER TABLE factory_settings ADD COLUMN corner_password TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN factory_settings.corner_password IS 'T 排/包角二次确认口令；空表示未设，示教器一律不放行';
