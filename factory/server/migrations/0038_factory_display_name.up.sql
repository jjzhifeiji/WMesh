-- 本厂显示名：认领或握手从 WAN 名录写入，给现场 Client 登录列表。

ALTER TABLE factory_lifecycle
    ADD COLUMN name TEXT; -- 本厂显示名，不当身份

COMMENT ON COLUMN factory_lifecycle.name IS '本厂显示名，认领或握手写入；给现场登录列表';
