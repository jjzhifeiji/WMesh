-- 示教器每次登录留下现场快照：版本、设备号、型号、WiFi 名；不含密码和令牌。

CREATE TABLE person_login_logs (
    id UUID PRIMARY KEY, -- 登录记录稳定身份
    person_id UUID NOT NULL REFERENCES people (id), -- 本厂登录人
    kind TEXT NOT NULL, -- pad=厂网登录；client=本机登录；mqtt=通道回连
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 服务端记下的时间
    app_version BIGINT NOT NULL DEFAULT 0, -- 示教器 versionCode；0 表示没报
    app_version_name TEXT NOT NULL DEFAULT '', -- 示教器 versionName
    device_serial TEXT NOT NULL DEFAULT '', -- 机械臂识别号；厂网登录时可能还没有
    device_model TEXT NOT NULL DEFAULT '', -- 平板型号
    device_manufacturer TEXT NOT NULL DEFAULT '', -- 平板厂商
    android_release TEXT NOT NULL DEFAULT '', -- 平板系统版本
    network_name TEXT NOT NULL DEFAULT '', -- 当时 WiFi 名；读不到则空
    client_id UUID, -- 已匹配的本机身份；未对臂为空
    CHECK (kind IN ('pad', 'client', 'mqtt'))
);

CREATE INDEX person_login_logs_person_time_idx ON person_login_logs (person_id, occurred_at DESC);

COMMENT ON TABLE person_login_logs IS '示教器登录现场快照；不含密码、令牌，不进 WAN';
COMMENT ON COLUMN person_login_logs.id IS '登录记录稳定身份';
COMMENT ON COLUMN person_login_logs.person_id IS '本厂登录人';
COMMENT ON COLUMN person_login_logs.kind IS 'pad=厂网登录；client=本机登录；mqtt=通道回连';
COMMENT ON COLUMN person_login_logs.occurred_at IS '服务端记下的时间';
COMMENT ON COLUMN person_login_logs.app_version IS '示教器 versionCode；0 表示没报';
COMMENT ON COLUMN person_login_logs.app_version_name IS '示教器 versionName';
COMMENT ON COLUMN person_login_logs.device_serial IS '机械臂识别号；厂网登录时可能还没有';
COMMENT ON COLUMN person_login_logs.device_model IS '平板型号';
COMMENT ON COLUMN person_login_logs.device_manufacturer IS '平板厂商';
COMMENT ON COLUMN person_login_logs.android_release IS '平板系统版本';
COMMENT ON COLUMN person_login_logs.network_name IS '当时 WiFi 名；读不到则空';
COMMENT ON COLUMN person_login_logs.client_id IS '已匹配的本机身份；未对臂为空';
