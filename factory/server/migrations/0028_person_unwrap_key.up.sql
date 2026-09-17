-- 解开工艺的钥匙跟人走；焊机只有编号，不再持钥。

ALTER TABLE people
    ADD COLUMN unwrap_key BYTEA; -- 登录人到站解封钥；未登录过为空

COMMENT ON COLUMN people.unwrap_key IS '登录人到站解封钥；不进 JSON、不进审计；焊机不持钥';
