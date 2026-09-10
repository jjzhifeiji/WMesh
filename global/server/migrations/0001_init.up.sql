-- WAN 库初始结构：只放唯一管理员、工厂名录、初始超管对账和 WAN 审计。
-- 不含厂内人员、组织、角色或日常口令。

CREATE TABLE factories (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE wan_admins (
    id UUID PRIMARY KEY,
    login_name TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX wan_admins_singleton ON wan_admins ((true)); -- 全表只能有一名 WAN 管理员

CREATE TABLE initial_super_admins (
    factory_id UUID PRIMARY KEY REFERENCES factories (id),
    person_id UUID NOT NULL,
    login_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    admin_id UUID NOT NULL REFERENCES wan_admins (id),
    token_hash TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE audit_events (
    id UUID PRIMARY KEY,
    actor_id UUID,
    claimed_login TEXT,
    factory_id UUID,
    org_unit_id UUID,
    org_path JSONB,
    action TEXT NOT NULL,
    target TEXT NOT NULL,
    result TEXT NOT NULL CHECK (result IN ('allow', 'deny')),
    time_source TEXT NOT NULL CHECK (time_source IN ('server')),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
