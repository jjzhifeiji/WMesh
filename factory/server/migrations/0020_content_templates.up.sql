-- 已收到的云端内容模版副本；按身份+修订并存，不得改已收正文。

CREATE TABLE content_template_replicas (
    id UUID NOT NULL, -- 与 WAN 原件相同
    revision BIGINT NOT NULL, -- 送达修订
    kind TEXT NOT NULL, -- process / project
    schema JSONB NOT NULL, -- 字段表
    digest BYTEA NOT NULL, -- 字段表 SHA-256
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- 本厂收到时间
    PRIMARY KEY (id, revision),
    CHECK (kind IN ('process', 'project')),
    CHECK (revision >= 1),
    CHECK (octet_length(digest) = 32),
    CHECK (jsonb_typeof(schema) = 'object')
);

COMMENT ON TABLE content_template_replicas IS '已下发到本厂的内容模版只读副本';
COMMENT ON COLUMN content_template_replicas.id IS '与 WAN 原件相同';
COMMENT ON COLUMN content_template_replicas.revision IS '送达修订';
COMMENT ON COLUMN content_template_replicas.kind IS 'process 工艺 / project 工程';
COMMENT ON COLUMN content_template_replicas.schema IS '字段表';
COMMENT ON COLUMN content_template_replicas.digest IS '字段表 SHA-256';
COMMENT ON COLUMN content_template_replicas.received_at IS '本厂收到时间';

CREATE FUNCTION prevent_template_replica_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'content template replicas cannot be physically deleted';
END;
$$;

CREATE TRIGGER content_template_replicas_no_delete
    BEFORE DELETE ON content_template_replicas
    FOR EACH ROW
    EXECUTE FUNCTION prevent_template_replica_delete();
