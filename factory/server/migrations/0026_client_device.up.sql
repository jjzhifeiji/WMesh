-- 本厂 Client 钉机械臂识别号，并持有到站重封用的解封钥。
-- 解封钥不进列表 JSON、不进审计；未作废设备号本厂唯一。

ALTER TABLE clients
    ADD COLUMN device_serial TEXT NOT NULL DEFAULT '', -- 从设备读到的机械臂识别号；未登记为空
    ADD COLUMN unwrap_key BYTEA; -- 到站重封解封钥；未登记为空

COMMENT ON COLUMN clients.device_serial IS '从设备读到的机械臂识别号；未登记为空，不当平台 Client 身份';
COMMENT ON COLUMN clients.unwrap_key IS '到站重封用的解封钥；不进 JSON、不进审计';

CREATE UNIQUE INDEX clients_bound_device_serial
    ON clients (device_serial)
    WHERE status = 'bound' AND device_serial <> ''; -- 未作废设备号本厂唯一
