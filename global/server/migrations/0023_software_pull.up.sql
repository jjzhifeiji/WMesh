-- 软件包改为三种独立发布；WAN 自记已装云端服务。分发表与签发钥不再写入。

ALTER TABLE software_releases DROP CONSTRAINT software_releases_kind_check;
ALTER TABLE software_releases ADD CONSTRAINT software_releases_kind_check
    CHECK (kind IN ('wan_service', 'factory_service', 'client_apk')); -- 三种包分开发

COMMENT ON COLUMN software_releases.kind IS 'wan_service / factory_service / client_apk';

CREATE TABLE software_state (
    kind TEXT PRIMARY KEY, -- 目前只记 wan_service
    installed_version BIGINT NOT NULL DEFAULT 0, -- 已确认落地的版本；0 表示未装
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 最近一次记已装
    CHECK (kind = 'wan_service'),
    CHECK (installed_version >= 0)
);

COMMENT ON TABLE software_state IS '本机已确认落地的云端服务版本';
COMMENT ON COLUMN software_state.kind IS '目前只记 wan_service';
COMMENT ON COLUMN software_state.installed_version IS '已确认落地的版本；0 表示未装';
COMMENT ON COLUMN software_state.updated_at IS '最近一次记已装';
