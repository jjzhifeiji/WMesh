-- WMESH_ADMIN_RESET 只生效一次：按配置重置过就记下所用口令的哈希，变量没删、口令没换时再启动不再重置。
-- +goose Up

CREATE TABLE wan_admin_resets (
    login_name TEXT PRIMARY KEY, -- 按配置重置过的 WAN 登录名
    password_hash TEXT NOT NULL, -- 重置所用口令的慢哈希，只用来认出同一口令，不存原文
    reset_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 最近一次按配置重置的时刻
);

COMMENT ON TABLE wan_admin_resets IS '按环境变量重置 WAN 管理员口令的一次性标记：每个登录名一行';
COMMENT ON COLUMN wan_admin_resets.login_name IS '按配置重置过的 WAN 登录名';
COMMENT ON COLUMN wan_admin_resets.password_hash IS '重置所用口令的慢哈希；同一口令再启动不再重置，换了口令才再生效';
COMMENT ON COLUMN wan_admin_resets.reset_at IS '最近一次按配置重置的时刻';
