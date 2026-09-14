-- 工厂/Client 短码与平台级工艺工程只读编号；已有行按创建顺序补齐。
-- 不改 UUID、修订、正文、摘要或审计原文。

CREATE TABLE origin_code_seq (
    kind TEXT PRIMARY KEY, -- factory / client
    next_n BIGINT NOT NULL, -- 下一个要发出的序号
    CHECK (kind IN ('factory', 'client')),
    CHECK (next_n >= 1)
);

COMMENT ON TABLE origin_code_seq IS '云端发厂短码与 Client 短码的计数，不回收';
COMMENT ON COLUMN origin_code_seq.kind IS 'factory 厂短码 / client 设备短码';
COMMENT ON COLUMN origin_code_seq.next_n IS '下一个要发出的序号';

INSERT INTO origin_code_seq (kind, next_n) VALUES ('factory', 1), ('client', 1);

CREATE TABLE asset_code_seq (
    kind TEXT PRIMARY KEY, -- process / project
    next_n BIGINT NOT NULL, -- 下一个 W 序号
    CHECK (kind IN ('process', 'project')),
    CHECK (next_n >= 1)
);

COMMENT ON TABLE asset_code_seq IS '云端平台级工艺/工程编号计数，不回收';
COMMENT ON COLUMN asset_code_seq.kind IS 'process 工艺 / project 工程';
COMMENT ON COLUMN asset_code_seq.next_n IS '下一个 W 序号';

INSERT INTO asset_code_seq (kind, next_n) VALUES ('process', 1), ('project', 1);

ALTER TABLE factories
    ADD COLUMN short_code TEXT; -- 本厂短码 F01…F99，创建后不改

UPDATE factories f
SET short_code = 'F' || lpad(s.n::text, 2, '0')
FROM (
    SELECT id, row_number() OVER (ORDER BY created_at, id) AS n
    FROM factories
) s
WHERE f.id = s.id;

UPDATE origin_code_seq
SET next_n = 1 + COALESCE((SELECT COUNT(*) FROM factories), 0)
WHERE kind = 'factory';

ALTER TABLE factories
    ALTER COLUMN short_code SET NOT NULL,
    ADD CONSTRAINT factories_short_code_check CHECK (short_code ~ '^F[0-9]{2}$');

CREATE UNIQUE INDEX factories_short_code_uq ON factories (short_code);

COMMENT ON COLUMN factories.short_code IS '本厂短码 F01…F99，创建后不改、不回收';

ALTER TABLE clients
    ADD COLUMN short_code TEXT; -- Client 短码 C0001…C9999，登记后不改

UPDATE clients c
SET short_code = 'C' || lpad(s.n::text, 4, '0')
FROM (
    SELECT id, row_number() OVER (ORDER BY created_at, id) AS n
    FROM clients
) s
WHERE c.id = s.id;

UPDATE origin_code_seq
SET next_n = 1 + COALESCE((SELECT COUNT(*) FROM clients), 0)
WHERE kind = 'client';

ALTER TABLE clients
    ALTER COLUMN short_code SET NOT NULL,
    ADD CONSTRAINT clients_short_code_check CHECK (short_code ~ '^C[0-9]{4}$');

CREATE UNIQUE INDEX clients_short_code_uq ON clients (short_code);

COMMENT ON COLUMN clients.short_code IS 'Client 短码 C0001…C9999，登记后不改、不回收';

ALTER TABLE assets
    ADD COLUMN code TEXT; -- 只读编号，创建后不改

UPDATE assets a
SET code = CASE a.kind
    WHEN 'process' THEN 'GY-W-' || lpad(s.n::text, 6, '0')
    ELSE 'GC-W-' || lpad(s.n::text, 6, '0')
END
FROM (
    SELECT id, kind, row_number() OVER (PARTITION BY kind ORDER BY created_at, id) AS n
    FROM assets
) s
WHERE a.id = s.id;

UPDATE asset_code_seq seq
SET next_n = 1 + COALESCE((
    SELECT COUNT(*) FROM assets WHERE kind = seq.kind
), 0);

ALTER TABLE assets
    ALTER COLUMN code SET NOT NULL,
    ADD CONSTRAINT assets_code_check CHECK (code ~ '^(GY|GC)-(W|F[0-9]{2}|C[0-9]{4})-[0-9]{6}$');

CREATE UNIQUE INDEX assets_code_uq ON assets (code);

COMMENT ON COLUMN assets.code IS '只读编号，创建后不改；不当身份';
