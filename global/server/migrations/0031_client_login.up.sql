-- 设备最近登录：厂端经通道保活上报该台最近一次示教器登录时间和 App 版本，WAN 只存最新一份给名录看。
-- +goose Up

ALTER TABLE clients
    ADD COLUMN last_login_at TIMESTAMPTZ, -- 厂端上报的最近一次登录时间；没报过为空
    ADD COLUMN app_version BIGINT NOT NULL DEFAULT 0, -- 最近登录时的示教器版本号；没报过为 0
    ADD COLUMN app_version_name TEXT NOT NULL DEFAULT ''; -- 最近登录时的示教器版本名；没报过为空

ALTER TABLE clients ADD CONSTRAINT clients_app_version_name_len
    CHECK (char_length(app_version_name) <= 80); -- 厂端已裁过，这里再兜底

COMMENT ON COLUMN clients.last_login_at IS '厂端上报的最近一次示教器登录时间；没报过为空';
COMMENT ON COLUMN clients.app_version IS '最近登录时的示教器版本号；没报过为 0';
COMMENT ON COLUMN clients.app_version_name IS '最近登录时的示教器版本名；没报过为空';
