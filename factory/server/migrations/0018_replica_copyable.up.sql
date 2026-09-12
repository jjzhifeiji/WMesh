-- 平台级副本可复制与源相同；本厂仍不得改。

DO $$
DECLARE r record;
BEGIN
    FOR r IN
        SELECT c.conname
        FROM pg_constraint c
        JOIN pg_class t ON c.conrelid = t.oid
        WHERE t.relname = 'asset_replicas' AND c.contype = 'c'
          AND pg_get_constraintdef(c.oid) ILIKE '%copyable%'
          AND pg_get_constraintdef(c.oid) ILIKE '%false%'
    LOOP
        EXECUTE format('ALTER TABLE asset_replicas DROP CONSTRAINT %I', r.conname);
    END LOOP;
END $$;

COMMENT ON COLUMN asset_replicas.copyable IS '与源相同；本厂不得改、不得因下发放宽';
