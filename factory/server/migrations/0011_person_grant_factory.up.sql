-- 人员离线授权改为厂级：本厂设备通用，不再绑特定 Client。

ALTER TABLE person_offline_grants DROP CONSTRAINT IF EXISTS person_offline_grants_client_id_fkey;
ALTER TABLE person_offline_grants DROP CONSTRAINT IF EXISTS person_offline_grants_person_id_client_id_revision_key;

-- 同一人同一修订只留最新一行，再收成厂级唯一。
DELETE FROM person_offline_grants a
WHERE EXISTS (
    SELECT 1 FROM person_offline_grants b
    WHERE b.person_id = a.person_id
      AND b.revision = a.revision
      AND (b.created_at > a.created_at OR (b.created_at = a.created_at AND b.id > a.id))
);

ALTER TABLE person_offline_grants DROP COLUMN client_id;

ALTER TABLE person_offline_grants
    ADD CONSTRAINT person_offline_grants_person_id_revision_key UNIQUE (person_id, revision);

COMMENT ON TABLE person_offline_grants IS '签给本厂账号的人员离线授权快照；本厂设备通用';
COMMENT ON COLUMN person_offline_grants.revision IS '该账号的厂级授权修订，只向前';
