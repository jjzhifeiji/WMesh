-- 本厂副本不再要求 WAN 签名；新拉入的包只核摘要。

ALTER TABLE software_replicas ALTER COLUMN signature DROP NOT NULL;
ALTER TABLE software_replicas DROP CONSTRAINT IF EXISTS software_replicas_signature_check;

COMMENT ON COLUMN software_replicas.signature IS '旧行可能有 WAN 签名；新拉入不再写';
