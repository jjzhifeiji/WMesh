-- 管理员可挂整厂；组织负责人仍只能挂节点。

DO $$
DECLARE
    cname text;
BEGIN
    SELECT conname INTO cname
    FROM pg_constraint
    WHERE conrelid = 'role_grants'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%org_admin%'
      AND pg_get_constraintdef(oid) LIKE '%org_lead%'
      AND pg_get_constraintdef(oid) LIKE '%scope_kind%';
    IF cname IS NOT NULL THEN
        EXECUTE format('ALTER TABLE role_grants DROP CONSTRAINT %I', cname);
    END IF;
END $$;

ALTER TABLE role_grants
    ADD CONSTRAINT role_grants_role_scope_check CHECK (
        (
            role = 'factory_super_admin'
            AND scope_kind = 'factory'
            AND org_unit_id IS NULL
        ) OR (
            role = 'org_lead'
            AND scope_kind = 'org_unit'
            AND org_unit_id IS NOT NULL
        ) OR (
            role IN ('org_admin', 'process_engineer', 'operator', 'auditor')
            AND (
                (scope_kind = 'factory' AND org_unit_id IS NULL)
                OR (scope_kind = 'org_unit' AND org_unit_id IS NOT NULL)
            )
        )
    );

COMMENT ON COLUMN role_grants.scope_kind IS 'factory 或 org_unit；管理员可挂整厂';
