-- Client 加人看的名字；公钥可空，等本机首次上线再登记。已有绑定行不改身份。

ALTER TABLE clients
    ADD COLUMN name TEXT NOT NULL DEFAULT 'Client'; -- 给人看的名字，可改，不当身份

ALTER TABLE clients ALTER COLUMN name DROP DEFAULT;

ALTER TABLE clients ADD CONSTRAINT clients_name_len CHECK (char_length(btrim(name)) BETWEEN 1 AND 64);

ALTER TABLE clients ALTER COLUMN public_key DROP NOT NULL;

ALTER TABLE clients DROP CONSTRAINT IF EXISTS clients_public_key_check;
ALTER TABLE clients ADD CONSTRAINT clients_public_key_len CHECK (public_key IS NULL OR octet_length(public_key) = 32);

DROP INDEX IF EXISTS clients_public_key_uq;
CREATE UNIQUE INDEX clients_public_key_uq ON clients (public_key) WHERE public_key IS NOT NULL; -- 有钥才唯一，空钥可多台待上线

COMMENT ON COLUMN clients.name IS '给人看的名字，可改，不当身份';
COMMENT ON COLUMN clients.public_key IS '本机公钥，首次上线登记；未上线为空，无私钥';
