-- 工程模版副本带名称，按身份取最高修订。

ALTER TABLE content_template_replicas
    ADD COLUMN name TEXT NOT NULL DEFAULT ''; -- 工程模版名称；工艺为空

COMMENT ON TABLE content_template_replicas IS '已下发到本厂的内容模版只读副本';
COMMENT ON COLUMN content_template_replicas.id IS '与 WAN 原件相同';
COMMENT ON COLUMN content_template_replicas.revision IS '送达修订';
COMMENT ON COLUMN content_template_replicas.kind IS 'process 工艺 / project 工程';
COMMENT ON COLUMN content_template_replicas.name IS '工程模版名称；工艺为空';
COMMENT ON COLUMN content_template_replicas.schema IS '字段表';
COMMENT ON COLUMN content_template_replicas.digest IS '字段表 SHA-256';
COMMENT ON COLUMN content_template_replicas.received_at IS '本厂收到时间';
