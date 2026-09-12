-- 工厂治理状态：停用/启用可逆；已认领的删除只注销，不物理删历史。

ALTER TABLE factories
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active', -- active / disabled / retired
    ADD COLUMN lifecycle_revision BIGINT NOT NULL DEFAULT 0, -- 厂端只接受更高修订
    ADD COLUMN status_changed_at TIMESTAMPTZ; -- 最近一次停用、启用或注销

ALTER TABLE factories
    ADD CONSTRAINT factories_status_check CHECK (status IN ('active', 'disabled', 'retired'));

COMMENT ON COLUMN factories.status IS 'active 有效 / disabled 停用 / retired 已注销';
COMMENT ON COLUMN factories.lifecycle_revision IS '厂端只接受更高修订';
COMMENT ON COLUMN factories.status_changed_at IS '最近一次停用、启用或注销';
