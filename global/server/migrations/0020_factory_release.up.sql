-- 厂端在线时自报的前端/服务版本；离线仍留上次值，名录只在在线时展示。

ALTER TABLE factories
    ADD COLUMN web_version BIGINT NOT NULL DEFAULT 0, -- 厂端前端版本号；0 表示还没报到
    ADD COLUMN web_version_name TEXT NOT NULL DEFAULT '', -- 厂端前端版本名
    ADD COLUMN service_version BIGINT NOT NULL DEFAULT 0, -- 厂端服务版本号；0 表示还没报到
    ADD COLUMN service_version_name TEXT NOT NULL DEFAULT ''; -- 厂端服务版本名

COMMENT ON COLUMN factories.web_version IS '厂端前端版本号；0 表示还没报到';
COMMENT ON COLUMN factories.web_version_name IS '厂端前端版本名';
COMMENT ON COLUMN factories.service_version IS '厂端服务版本号；0 表示还没报到';
COMMENT ON COLUMN factories.service_version_name IS '厂端服务版本名';
