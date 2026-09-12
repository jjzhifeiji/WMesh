-- 本厂设备记下当前使用人，供列表展示；不是人员授权，不进 WAN。

ALTER TABLE clients
    ADD COLUMN operator_id UUID REFERENCES people (id);

COMMENT ON COLUMN clients.operator_id IS '当前在本机登录的本厂账号；无人或已作废为空';
