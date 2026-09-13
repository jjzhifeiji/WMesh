-- 本厂内容主钥只以租约钥包装后落盘；L 与 MK 原文不进库。

CREATE TABLE content_master (
    id SMALLINT PRIMARY KEY DEFAULT 1, -- 单行
    wrapped_mk BYTEA NOT NULL, -- 用当前租约钥 L 包着的 MK
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 最近一次包装
    CHECK (id = 1)
);

COMMENT ON TABLE content_master IS '本厂内容主钥的包装件；解包钥只在进程内存';
COMMENT ON COLUMN content_master.id IS '单行';
COMMENT ON COLUMN content_master.wrapped_mk IS '用当前租约钥 L 包着的 MK';
COMMENT ON COLUMN content_master.updated_at IS '最近一次包装';
