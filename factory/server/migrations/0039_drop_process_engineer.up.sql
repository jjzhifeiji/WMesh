-- 收回已无的工艺工程师角色。历史行保留 role 字面量，不改旧约束。

UPDATE role_grants
SET status = 'revoked', revoked_at = now()
WHERE role = 'process_engineer' AND status = 'active';
