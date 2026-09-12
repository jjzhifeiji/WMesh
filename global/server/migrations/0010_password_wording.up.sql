-- 库内注释用语：口令改为密码；不改列类型或已落库数据。

COMMENT ON COLUMN wan_admins.password_hash IS '日常密码哈希，只存在 WAN 库';
COMMENT ON TABLE initial_super_admins IS '交付对账用的初始超管身份，不含日常密码';
