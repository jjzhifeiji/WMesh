-- 设备识别号大小写不论：库里统一存大写，唯一索引也按大写判重，fa:ke 与 FA:KE 只能登记一台。
-- 已有大小写不同的同号：最早登记的那台留号，后登记的清空识别号（空号不当身份），由管理员核对后重新登记。
-- +goose Up

UPDATE clients AS c
SET device_serial = ''
WHERE c.device_serial <> ''
  AND EXISTS (
      SELECT 1 FROM clients AS o
      WHERE o.id <> c.id
        AND o.device_serial <> ''
        AND upper(o.device_serial) = upper(c.device_serial)
        AND (o.created_at, o.id) < (c.created_at, c.id)
  ); -- 大小写撞号的后来者先清空，免得收大写时撞唯一索引

UPDATE clients
SET device_serial = upper(device_serial)
WHERE device_serial <> upper(device_serial); -- 存量识别号收成大写，和示教器比对口径一致

DROP INDEX IF EXISTS clients_device_serial_uq;

CREATE UNIQUE INDEX clients_device_serial_upper_uq
    ON clients (upper(device_serial))
    WHERE device_serial <> ''; -- 有号则按大写全局唯一

COMMENT ON COLUMN clients.device_serial IS '机械臂识别号，登记时收成大写存放、大小写不论判重；空表示未填，不当平台 Client 身份';
