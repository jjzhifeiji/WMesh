-- 本厂治理状态：由 WAN 通道下发，修订只向前。不存厂内人员。

CREATE TABLE factory_lifecycle (
    id SMALLINT PRIMARY KEY DEFAULT 1, -- 全表一行
    status TEXT NOT NULL, -- active / disabled / retired
    revision BIGINT NOT NULL, -- 已接受的 WAN 修订，只向前
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 最近一次落地
    CHECK (id = 1),
    CHECK (status IN ('active', 'disabled', 'retired')),
    CHECK (revision >= 0)
);

INSERT INTO factory_lifecycle (id, status, revision) VALUES (1, 'active', 0);

COMMENT ON TABLE factory_lifecycle IS '本厂治理状态，由 WAN 下发';
COMMENT ON COLUMN factory_lifecycle.id IS '全表一行';
COMMENT ON COLUMN factory_lifecycle.status IS 'active 有效 / disabled 停用 / retired 已注销';
COMMENT ON COLUMN factory_lifecycle.revision IS '已接受的 WAN 修订，只向前';
COMMENT ON COLUMN factory_lifecycle.updated_at IS '最近一次落地';
