-- 厂库初始结构：本厂账号、组织树、分配、角色、会话、审计。
-- 新厂套用同一套迁移；按工厂稳定身份选库。

CREATE TABLE people (
    id UUID PRIMARY KEY,
    login_name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'disabled')),
    password_hash TEXT,
    activation_token_hash TEXT,
    is_initial_super_admin BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (login_name)
);

CREATE UNIQUE INDEX people_one_initial_sa ON people ((true)) WHERE is_initial_super_admin; -- 一厂一名初始超管

CREATE TABLE org_types (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE org_units (
    id UUID PRIMARY KEY,
    org_type_id UUID NOT NULL REFERENCES org_types (id),
    parent_id UUID REFERENCES org_units (id),
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (parent_id IS DISTINCT FROM id)
);

CREATE INDEX org_units_parent_idx ON org_units (parent_id);

CREATE TABLE assignments (
    id UUID PRIMARY KEY,
    person_id UUID NOT NULL REFERENCES people (id),
    org_unit_id UUID NOT NULL REFERENCES org_units (id),
    status TEXT NOT NULL CHECK (status IN ('active', 'ended')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ,
    CHECK (
        (status = 'active' AND ended_at IS NULL)
        OR (status = 'ended' AND ended_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX assignments_one_active ON assignments (person_id, org_unit_id) WHERE status = 'active';

CREATE TABLE role_grants (
    id UUID PRIMARY KEY,
    person_id UUID NOT NULL REFERENCES people (id),
    role TEXT NOT NULL CHECK (role IN (
        'factory_super_admin',
        'org_admin',
        'org_lead',
        'process_engineer',
        'operator',
        'auditor'
    )),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('factory', 'org_unit')),
    org_unit_id UUID REFERENCES org_units (id),
    status TEXT NOT NULL CHECK (status IN ('active', 'revoked')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
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
    id UUID PRIMARY KEY,
    person_id UUID NOT NULL REFERENCES people (id),
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
