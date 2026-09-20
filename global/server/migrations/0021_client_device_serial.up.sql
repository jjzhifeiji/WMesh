-- 设备识别号：WAN 登记时填写，分厂后随绑定推给厂端；不当平台 Client 身份。

ALTER TABLE clients
    ADD COLUMN device_serial TEXT NOT NULL DEFAULT ''; -- 机械臂识别号；未填为空，不当身份

ALTER TABLE clients ADD CONSTRAINT clients_device_serial_len
    CHECK (char_length(device_serial) <= 128); -- 人手填写，限制长度

CREATE UNIQUE INDEX clients_device_serial_uq
    ON clients (device_serial)
    WHERE device_serial <> ''; -- 有号则全局唯一

COMMENT ON COLUMN clients.device_serial IS '机械臂识别号，登记时填写；空表示未填，不当平台 Client 身份';
