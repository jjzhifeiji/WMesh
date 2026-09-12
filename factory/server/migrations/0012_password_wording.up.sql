-- 库内注释用语：口令改为密码；不改列类型或已落库数据。

COMMENT ON COLUMN people.password_hash IS '日常密码哈希，只存在本厂；激活前为空';
COMMENT ON COLUMN people.activation_token_hash IS '一次性激活密码哈希，激活后清空';
COMMENT ON COLUMN person_offline_grants.password_hash IS '该人密码验证材料副本，不是全厂账号库';
