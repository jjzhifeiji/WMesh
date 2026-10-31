-- 组织分配并进人员：每人至多一个所属组织，挂在人员行上。
-- 角色不再带作用域，只留超管、管理员、操作员；账号不再有待启用和激活码。
-- 焊事实只绑人，组织按人现在的所属组织算；资产不再记组织路径。
-- +goose Up

ALTER TABLE people ADD COLUMN org_unit_id UUID REFERENCES org_units (id);

UPDATE people AS p
SET org_unit_id = a.org_unit_id
FROM assignments AS a
WHERE a.person_id = p.id AND a.status = 'active';

DROP TABLE assignments;

COMMENT ON COLUMN people.org_unit_id IS '所属组织节点；空表示厂直属。只用于按组织统计，不给权限';
COMMENT ON TABLE people IS '本厂自然人账号，固定只属于本厂库；可归一个所属组织节点';

DELETE FROM people WHERE status = 'pending' AND NOT is_initial_super_admin
    AND NOT EXISTS (SELECT 1 FROM sessions WHERE sessions.person_id = people.id);
UPDATE people SET status = 'disabled' WHERE status = 'pending';

-- +goose StatementBegin
DO $$
DECLARE
    cname text;
BEGIN
    FOR cname IN
        SELECT conname FROM pg_constraint
        WHERE conrelid = 'people'::regclass AND contype = 'c'
          AND pg_get_constraintdef(oid) LIKE '%pending%'
    LOOP
        EXECUTE format('ALTER TABLE people DROP CONSTRAINT %I', cname);
    END LOOP;
    FOR cname IN
        SELECT conname FROM pg_constraint
        WHERE conrelid = 'role_grants'::regclass AND contype = 'c'
          AND (pg_get_constraintdef(oid) LIKE '%scope_kind%' OR pg_get_constraintdef(oid) LIKE '%org_lead%')
    LOOP
        EXECUTE format('ALTER TABLE role_grants DROP CONSTRAINT %I', cname);
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE people ADD CONSTRAINT people_status_check CHECK (status IN ('active', 'disabled'));
ALTER TABLE people DROP COLUMN activation_token_hash;
COMMENT ON COLUMN people.status IS 'active / disabled';
COMMENT ON COLUMN people.password_hash IS '日常密码哈希，只存在本厂';

DELETE FROM role_grants WHERE role NOT IN ('factory_super_admin', 'org_admin', 'operator', 'auditor');
UPDATE role_grants SET role = 'org_admin' WHERE role = 'auditor';
DROP INDEX IF EXISTS role_grants_active_unique;
DELETE FROM role_grants AS a
WHERE a.status = 'active'
  AND EXISTS (
    SELECT 1 FROM role_grants AS b
    WHERE b.person_id = a.person_id AND b.role = a.role AND b.status = 'active'
      AND (b.created_at < a.created_at OR (b.created_at = a.created_at AND b.id < a.id))
  );
ALTER TABLE role_grants DROP COLUMN scope_kind;
ALTER TABLE role_grants DROP COLUMN org_unit_id;
ALTER TABLE role_grants ADD CONSTRAINT role_grants_role_check
    CHECK (role IN ('factory_super_admin', 'org_admin', 'operator'));
CREATE UNIQUE INDEX role_grants_active_person_role ON role_grants (person_id, role) WHERE status = 'active'; -- 同人同角色只留一条有效
COMMENT ON COLUMN role_grants.role IS '固定角色：超管 / 管理员（工艺与审计）/ 操作员；都覆盖整厂';

ALTER TABLE weld_facts DROP COLUMN org_unit_id;
ALTER TABLE weld_facts DROP COLUMN org_path;

ALTER TABLE assets DROP COLUMN org_unit_id;
ALTER TABLE assets DROP COLUMN org_path;

DROP TABLE personal_asset_stubs;
