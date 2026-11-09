-- 现场发起、云端核对后批准的申请：新厂接入和找回初始超管密码共用一张表。
-- 申请密钥只存哈希；批准新厂时这份哈希直接成为该厂的 enrollment_token_hash。日常密码不进云端。
-- +goose Up

CREATE TABLE onboarding_requests (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('enroll', 'reset_sa')),
    status TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'used')),
    secret_hash TEXT NOT NULL,
    verify_code TEXT NOT NULL,
    factory_id UUID REFERENCES factories (id) ON DELETE SET NULL,
    factory_name TEXT NOT NULL,
    sa_login TEXT NOT NULL,
    sa_display TEXT NOT NULL,
    source_addr TEXT NOT NULL DEFAULT '',
    service_version_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    decided_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX onboarding_requests_secret_uq ON onboarding_requests (secret_hash);
CREATE INDEX onboarding_requests_open_idx ON onboarding_requests (status, expires_at);

COMMENT ON TABLE onboarding_requests IS '现场发起、云端批准的新厂接入与初始超管找回申请';
COMMENT ON COLUMN onboarding_requests.id IS '申请稳定身份';
COMMENT ON COLUMN onboarding_requests.kind IS 'enroll 新厂接入 / reset_sa 找回初始超管密码';
COMMENT ON COLUMN onboarding_requests.status IS 'pending / approved / rejected / used';
COMMENT ON COLUMN onboarding_requests.secret_hash IS '申请密钥哈希，原文只回给现场一次';
COMMENT ON COLUMN onboarding_requests.verify_code IS '六位核对码，现场和云端各显示一份，人工比对';
COMMENT ON COLUMN onboarding_requests.factory_id IS '批准后或找回时对应的工厂';
COMMENT ON COLUMN onboarding_requests.factory_name IS '工厂显示名';
COMMENT ON COLUMN onboarding_requests.sa_login IS '初始超管登录名';
COMMENT ON COLUMN onboarding_requests.sa_display IS '初始超管显示名';
COMMENT ON COLUMN onboarding_requests.source_addr IS '发起申请的来源地址';
COMMENT ON COLUMN onboarding_requests.service_version_name IS '发起方厂端服务版本名';
COMMENT ON COLUMN onboarding_requests.created_at IS '申请时间';
COMMENT ON COLUMN onboarding_requests.expires_at IS '到期时间；过期的申请不能再批准或使用';
COMMENT ON COLUMN onboarding_requests.decided_at IS '批准或拒绝的时间';
