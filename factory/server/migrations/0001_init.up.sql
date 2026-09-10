-- 厂库初始结构：本厂账号、组织树、分配、角色、会话、审计。
-- 新厂套用同一套迁移；按工厂稳定身份选库。

CREATE TABLE people (
    id UUID PRIMARY KEY, -- 稳定身份，改名也不变
    login_name TEXT NOT NULL, -- 本厂内唯一登录名，不是身份
    display_name TEXT NOT NULL, -- 显示名，可改
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'disabled')), -- pending / active / disabled
    password_hash TEXT, -- 日常口令哈希，只存在本厂；激活前为空
    activation_token_hash TEXT, -- 一次性激活口令哈希，激活后清空
    is_initial_super_admin BOOLEAN NOT NULL DEFAULT false, -- 本厂唯一的 WAN 下发初始超管
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 账号创建时间
    UNIQUE (login_name)
);

CREATE UNIQUE INDEX people_one_initial_sa ON people ((true)) WHERE is_initial_super_admin; -- 一厂一名初始超管

CREATE TABLE org_types (
    id UUID PRIMARY KEY, -- 组织类型稳定身份
    name TEXT NOT NULL, -- 显示名，不当身份
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')), -- active / disabled；有有效节点时不能停
    created_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 创建时间
);

CREATE TABLE org_units (
    id UUID PRIMARY KEY, -- 节点稳定身份
    org_type_id UUID NOT NULL REFERENCES org_types (id), -- 必须挂本厂已有组织类型
    parent_id UUID REFERENCES org_units (id), -- 空表示直接挂在工厂下
    name TEXT NOT NULL, -- 显示名，改名不改历史快照
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')), -- 停用后不能再当新工作上下文
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 创建时间
    CHECK (parent_id IS DISTINCT FROM id)
);

CREATE INDEX org_units_parent_idx ON org_units (parent_id);

CREATE TABLE assignments (
    id UUID PRIMARY KEY, -- 分配关系稳定身份
    person_id UUID NOT NULL REFERENCES people (id), -- 本厂人员
    org_unit_id UUID NOT NULL REFERENCES org_units (id), -- 分配到的组织节点
    status TEXT NOT NULL CHECK (status IN ('active', 'ended')), -- active 或 ended；取消不删行
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 分配开始时间
    ended_at TIMESTAMPTZ, -- 取消分配的时间；有效分配必须为空
    CHECK (
        (status = 'active' AND ended_at IS NULL)
        OR (status = 'ended' AND ended_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX assignments_one_active ON assignments (person_id, org_unit_id) WHERE status = 'active';

CREATE TABLE role_grants (
    id UUID PRIMARY KEY, -- 授予记录稳定身份
    person_id UUID NOT NULL REFERENCES people (id), -- 被授予的本厂人员
    role TEXT NOT NULL CHECK (role IN (
        'factory_super_admin',
        'org_admin',
        'org_lead',
        'process_engineer',
        'operator',
        'auditor'
    )), -- 六种固定角色之一
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('factory', 'org_unit')), -- factory 或 org_unit
    org_unit_id UUID REFERENCES org_units (id), -- Factory 作用域必须为空
    status TEXT NOT NULL CHECK (status IN ('active', 'revoked')), -- active 或 revoked
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 授予时间
    revoked_at TIMESTAMPTZ, -- 收回时间；有效授予必须为空
    CHECK (
        (
            role = 'factory_super_admin'
            AND scope_kind = 'factory'
            AND org_unit_id IS NULL
        ) OR (
            role IN ('org_admin', 'org_lead')
            AND scope_kind = 'org_unit'
            AND org_unit_id IS NOT NULL
        ) OR (
            role IN ('process_engineer', 'operator', 'auditor')
            AND (
                (scope_kind = 'factory' AND org_unit_id IS NULL)
                OR (scope_kind = 'org_unit' AND org_unit_id IS NOT NULL)
            )
        )
    ),
    CHECK (
        (status = 'active' AND revoked_at IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX role_grants_active_unique
    ON role_grants (person_id, role, scope_kind, COALESCE(org_unit_id, '00000000-0000-0000-0000-000000000000'))
    WHERE status = 'active';

CREATE TABLE sessions (
    id UUID PRIMARY KEY, -- 会话稳定身份
    person_id UUID NOT NULL REFERENCES people (id), -- 持有该会话的本厂人员
    token_hash TEXT NOT NULL UNIQUE, -- 会话令牌哈希，不存原文
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 会话建立时间
    expires_at TIMESTAMPTZ NOT NULL -- 过期后立刻无效
);

CREATE TABLE audit_events (
    id UUID PRIMARY KEY, -- 审计条目稳定身份
    actor_id UUID, -- 操作者稳定身份；认不出则为空
    claimed_login TEXT, -- 被声明的登录标识
    factory_id UUID, -- 所属工厂；本厂库写入时即本厂
    org_unit_id UUID, -- 相关组织节点；无则空
    org_path JSONB, -- 当时组织路径快照；无则空
    action TEXT NOT NULL, -- 做了什么操作
    target TEXT NOT NULL, -- 作用对象
    result TEXT NOT NULL CHECK (result IN ('allow', 'deny')), -- allow 或 deny
    time_source TEXT NOT NULL CHECK (time_source IN ('server')), -- 时间来源，本阶段固定 server
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now() -- 服务端记录时间
);
