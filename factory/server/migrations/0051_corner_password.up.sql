-- 包角二次确认口令不再写死在示教器包里，改由本厂策略下发，超管可改。
-- +goose Up

ALTER TABLE factory_settings
    ADD COLUMN corner_password TEXT NOT NULL DEFAULT 'bd888888'; -- 包角二次确认口令，默认沿用原值

ALTER TABLE factory_settings
    ADD CONSTRAINT factory_settings_corner_password_check CHECK (length(btrim(corner_password)) > 0);

COMMENT ON COLUMN factory_settings.corner_password IS '包角二次确认口令；随策略下发，示教器离线按本地缓存核对';
