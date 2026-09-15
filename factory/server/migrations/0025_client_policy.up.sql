-- 把本厂 Client 策略并进 factory_settings 单行：修订、缓存范围、解封钥是否落盘、时效与扩展键。
-- 不另起第二张表；不改已有 max_cached_projects 取值。

ALTER TABLE factory_settings
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 0, -- 策略修订；Client 只接受更高
    ADD COLUMN cache_scope TEXT NOT NULL DEFAULT 'all', -- current：只缓存当前激活工程；all：该人获准的全部（仍受上限）
    ADD COLUMN persist_unwrap_key BOOLEAN NOT NULL DEFAULT false, -- 解封钥包装材料可否落盘，禁止明文
    ADD COLUMN key_ttl_seconds BIGINT NOT NULL DEFAULT 0, -- 解封钥时效秒；0 表示仅进程存活
    ADD COLUMN extra JSONB NOT NULL DEFAULT '{}'::jsonb; -- 本厂扩展键；Client 忽略未知

ALTER TABLE factory_settings
    ADD CONSTRAINT factory_settings_revision_check CHECK (revision >= 0),
    ADD CONSTRAINT factory_settings_cache_scope_check CHECK (cache_scope IN ('current', 'all')),
    ADD CONSTRAINT factory_settings_key_ttl_check CHECK (key_ttl_seconds >= 0),
    ADD CONSTRAINT factory_settings_extra_object CHECK (jsonb_typeof(extra) = 'object');

COMMENT ON TABLE factory_settings IS '本厂一份 Client 策略：缓存上限与范围、解封钥落盘与时效；对本厂全部 Client';
COMMENT ON COLUMN factory_settings.revision IS '策略修订；每次超管写入加一，Client 只接受更高';
COMMENT ON COLUMN factory_settings.cache_scope IS 'current：只缓存当前激活工程；all：该人获准的全部（仍受上限）';
COMMENT ON COLUMN factory_settings.persist_unwrap_key IS '解封钥包装材料可否落盘；禁止工艺明文和钥原文落盘';
COMMENT ON COLUMN factory_settings.key_ttl_seconds IS '解封钥时效秒；0 表示仅进程存活，退出即清';
COMMENT ON COLUMN factory_settings.extra IS '本厂扩展键 JSON 对象；Client 不认识的忽略';
