-- 登录日志补设备显示名快照；改名不改已经记下的那次现场。

ALTER TABLE person_login_logs
    ADD COLUMN client_name TEXT NOT NULL DEFAULT ''; -- 当时匹配到的本厂设备名；未对臂为空

COMMENT ON COLUMN person_login_logs.client_name IS '当时匹配到的本厂设备名快照；未对臂为空';
