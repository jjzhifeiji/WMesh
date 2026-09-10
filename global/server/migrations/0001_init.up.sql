-- WAN 库初始结构：只放唯一管理员、工厂名录、初始超管对账和 WAN 审计。
-- 不含厂内人员、组织、角色或日常口令。

CREATE TABLE factories (
    id UUID PRIMARY KEY, -- 工厂稳定身份，也用来选厂库
    name TEXT NOT NULL, -- 工厂显示名，不当身份
    created_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 名录入库时间
);

CREATE TABLE wan_admins (
    id UUID PRIMARY KEY, -- WAN 管理员稳定身份
    login_name TEXT NOT NULL UNIQUE, -- WAN 登录名，全库唯一
    password_hash TEXT NOT NULL, -- 日常口令哈希，只存在 WAN 库
    created_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 账号创建时间
);

CREATE UNIQUE INDEX wan_admins_singleton ON wan_admins ((true)); -- 全表只能有一名 WAN 管理员

CREATE TABLE initial_super_admins (
    factory_id UUID PRIMARY KEY REFERENCES factories (id), -- 一厂只能绑一名初始超管
    person_id UUID NOT NULL, -- 落在目标厂库里的账号身份
    login_name TEXT NOT NULL, -- 交付时的登录名，不是秘密
    created_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 对账记录写入时间
);

CREATE TABLE sessions (
    id UUID PRIMARY KEY, -- 会话稳定身份
    admin_id UUID NOT NULL REFERENCES wan_admins (id), -- 持有该会话的 WAN 管理员
    token_hash TEXT NOT NULL UNIQUE, -- 会话令牌哈希，不存原文
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 会话建立时间
    expires_at TIMESTAMPTZ NOT NULL -- 过期后此会话立刻无效
);

CREATE TABLE audit_events (
    id UUID PRIMARY KEY, -- 审计条目稳定身份
    actor_id UUID, -- 操作者稳定身份；认不出则为空
    claimed_login TEXT, -- 被声明的登录标识
    factory_id UUID, -- 相关工厂；无则空
    org_unit_id UUID, -- 相关组织节点；WAN 侧通常为空
    org_path JSONB, -- 当时组织路径快照；无则空
    action TEXT NOT NULL, -- 做了什么操作
    target TEXT NOT NULL, -- 作用对象
    result TEXT NOT NULL CHECK (result IN ('allow', 'deny')), -- allow 或 deny
    time_source TEXT NOT NULL CHECK (time_source IN ('server')), -- 时间来源，本阶段固定 server
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 服务端记录时间
);
