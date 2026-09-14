-- 本厂短码、Client 短码、工艺工程只读编号；已有自建行按本厂计数补齐。
-- 已有平台级副本不编造云端号，等下次下发带上。不改 UUID、修订、正文、摘要或审计原文。

CREATE TABLE asset_code_seq (
    kind TEXT PRIMARY KEY, -- process / project
    next_n BIGINT NOT NULL, -- 下一个本厂序号
    CHECK (kind IN ('process', 'project')),
    CHECK (next_n >= 1)
);

COMMENT ON TABLE asset_code_seq IS '本厂自建工艺/工程编号计数，不回收';
COMMENT ON COLUMN asset_code_seq.kind IS 'process 工艺 / project 工程';
COMMENT ON COLUMN asset_code_seq.next_n IS '下一个本厂序号';

INSERT INTO asset_code_seq (kind, next_n) VALUES ('process', 1), ('project', 1);

ALTER TABLE factory_lifecycle
    ADD COLUMN short_code TEXT; -- 本厂短码，认领后写入；空则不得新建

ALTER TABLE factory_lifecycle
    ADD CONSTRAINT factory_lifecycle_short_code_check CHECK (short_code IS NULL OR short_code ~ '^F[0-9]{2}$');

COMMENT ON COLUMN factory_lifecycle.short_code IS '本厂短码，认领或握手写入后不改';

ALTER TABLE clients
    ADD COLUMN short_code TEXT; -- Client 短码，随绑定补齐

ALTER TABLE clients
    ADD CONSTRAINT clients_short_code_check CHECK (short_code IS NULL OR short_code ~ '^C[0-9]{4}$');

COMMENT ON COLUMN clients.short_code IS 'Client 短码，随绑定补齐后不改';

CREATE TABLE asset_codes (
    id UUID PRIMARY KEY, -- 资产稳定身份
    code TEXT NOT NULL, -- 该身份的只读编号
    CHECK (code ~ '^(GY|GC)-(W|F[0-9]{2}|C[0-9]{4})-[0-9]{6}$')
);

CREATE UNIQUE INDEX asset_codes_code_uq ON asset_codes (code);

COMMENT ON TABLE asset_codes IS '身份级编号登记，覆盖本厂自建与已收副本';
COMMENT ON COLUMN asset_codes.id IS '资产稳定身份';
COMMENT ON COLUMN asset_codes.code IS '该身份的只读编号，不跟修订走';

ALTER TABLE assets
    ADD COLUMN code TEXT; -- 只读编号，创建后不改

-- 尚无本厂短码时先占 F00，仅用于迁移已有自建行；认领后不得再用 F00 发号。
UPDATE assets a
SET code = CASE a.kind
    WHEN 'process' THEN 'GY-F00-' || lpad(s.n::text, 6, '0')
    ELSE 'GC-F00-' || lpad(s.n::text, 6, '0')
END
FROM (
    SELECT id, kind, row_number() OVER (PARTITION BY kind ORDER BY created_at, id) AS n
    FROM assets
) s
WHERE a.id = s.id;

INSERT INTO asset_codes (id, code)
SELECT id, code FROM assets
WHERE code IS NOT NULL
ON CONFLICT (id) DO NOTHING;

UPDATE asset_code_seq seq
SET next_n = 1 + COALESCE((
    SELECT COUNT(*) FROM assets WHERE kind = seq.kind
), 0);

ALTER TABLE assets
    ALTER COLUMN code SET NOT NULL,
    ADD CONSTRAINT assets_code_check CHECK (code ~ '^(GY|GC)-(W|F[0-9]{2}|C[0-9]{4})-[0-9]{6}$');

CREATE UNIQUE INDEX assets_code_uq ON assets (code);

COMMENT ON COLUMN assets.code IS '只读编号，创建后不改；不当身份';

ALTER TABLE asset_replicas
    ADD COLUMN code TEXT; -- 与云端原件相同；旧副本可空

ALTER TABLE asset_replicas
    ADD CONSTRAINT asset_replicas_code_check CHECK (code IS NULL OR code ~ '^(GY|GC)-(W|F[0-9]{2}|C[0-9]{4})-[0-9]{6}$');

COMMENT ON COLUMN asset_replicas.code IS '与云端原件相同；同一身份多修订同号';
