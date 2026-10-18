-- 本厂 Client 记下上次收到的 WAN 设备名；同修订重放时据此判断 WAN 是否真改了名，免得冲掉本厂超管的改名。
-- 旧行为空：首次同修订重放只记下，不覆盖本地名。修订升高照旧以 WAN 为准。

ALTER TABLE clients
    ADD COLUMN wan_name TEXT; -- 上次收到的 WAN 设备名；旧行为空

COMMENT ON COLUMN clients.wan_name IS '上次收到的 WAN 设备名，只用来判断同修订重放时 WAN 是否改名；旧行为空，不当身份';
