-- WAN 删掉的工程模版在厂端标成已撤下：副本不得物理删除，只记撤下时刻，列表和新建不再用它。
-- +goose Up

ALTER TABLE content_template_replicas ADD COLUMN retired_at TIMESTAMPTZ;

COMMENT ON COLUMN content_template_replicas.retired_at IS 'WAN 已删除、本厂按全量索引撤下的时刻；空表示仍在用';
