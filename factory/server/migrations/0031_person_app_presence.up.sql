-- 人员页展示示教器登录在线、最后见到和正在跑的 APK 版本；不进 software_state。

ALTER TABLE sessions
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'web'; -- web=管理端；app=示教器

ALTER TABLE sessions
    ADD CONSTRAINT sessions_kind_check CHECK (kind IN ('web', 'app'));

COMMENT ON COLUMN sessions.kind IS 'web=管理端登录；app=示教器登录，名册在线按未过期 app 会话计';

ALTER TABLE people
    ADD COLUMN app_last_seen_at TIMESTAMPTZ, -- 最近一次示教器登录或 MQTT 见到
    ADD COLUMN app_version BIGINT NOT NULL DEFAULT 0, -- 示教器自报 versionCode；0 表示还没报到
    ADD COLUMN app_version_name TEXT NOT NULL DEFAULT ''; -- 示教器自报 versionName

COMMENT ON COLUMN people.app_last_seen_at IS '最近一次示教器登录或 MQTT 见到；未用过 APP 为空';
COMMENT ON COLUMN people.app_version IS '示教器自报 versionCode；0 表示还没报到；不进 software_state';
COMMENT ON COLUMN people.app_version_name IS '示教器自报 versionName；不当已确认安装版本';
