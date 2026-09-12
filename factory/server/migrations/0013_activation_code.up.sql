-- 激活码改为 8 位数字；只改库内注释，不改列或已落库哈希。

COMMENT ON COLUMN people.activation_token_hash IS '一次性 8 位数字激活码哈希，激活后清空';
