-- 设备名只由 WAN 定，厂端另起昵称：之前厂端改过的名字挪进昵称，名字回到 WAN 下发的那个。
-- +goose Up

ALTER TABLE clients ADD COLUMN nickname TEXT NOT NULL DEFAULT '';

UPDATE clients SET nickname = name, name = wan_name
WHERE wan_name IS NOT NULL AND name <> wan_name;

COMMENT ON COLUMN clients.nickname IS '厂端起的昵称，空表示没起；设备名本身只由 WAN 改';
COMMENT ON COLUMN clients.name IS 'WAN 下发的设备名，厂端只读';
