-- 撤掉包角口令：包角工艺改按普通工艺管理，加密与否走可复制口径，不再需要单独口令。
-- +goose Up

ALTER TABLE factory_settings
    DROP CONSTRAINT IF EXISTS factory_settings_corner_password_check;

ALTER TABLE factory_settings
    DROP COLUMN IF EXISTS corner_password;
