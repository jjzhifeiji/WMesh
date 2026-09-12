-- 平台级可复制与厂级同一套：草稿可改，可用后只许收紧。新建仍默认为否。

DO $$
DECLARE r record;
BEGIN
    FOR r IN
        SELECT c.conname
        FROM pg_constraint c
        JOIN pg_class t ON c.conrelid = t.oid
        WHERE t.relname = 'assets' AND c.contype = 'c'
          AND pg_get_constraintdef(c.oid) ILIKE '%copyable%'
          AND pg_get_constraintdef(c.oid) ILIKE '%false%'
    LOOP
        EXECUTE format('ALTER TABLE assets DROP CONSTRAINT %I', r.conname);
    END LOOP;
END $$;

COMMENT ON COLUMN assets.copyable IS '可否被上一级复制；草稿可改，可用后只许收紧；新建默认为否';
