-- 工艺模版从一份改成多份独立副本：云端下发的每份工艺模版各自一行，按全集撤下已删的，和工程模版一样。
-- 已收的那一份改成预置「焊接参数」的固定身份，和云端迁移后的身份对上；工艺记下自己属于哪份模版。
-- +goose Up

-- 以前云端的工艺模版身份是随机的，云端每重置一次就换一个；本厂副本只增不删，可能留着几个旧身份、修订相同的副本。
-- 只把最近收到的那个身份换成预置「焊接参数」的身份和名称，修订不变，云端重推同修订时算已收；
-- 其余旧身份的副本标成已撤下，不改身份，免得同修订撞主键。
UPDATE content_template_replicas
SET id = '55555555-5555-4555-8555-555555555555', name = '焊接参数'
WHERE kind = 'process'
  AND id = (SELECT id FROM content_template_replicas WHERE kind = 'process' ORDER BY received_at DESC, revision DESC, id LIMIT 1);

UPDATE content_template_replicas
SET retired_at = now()
WHERE kind = 'process'
  AND id <> '55555555-5555-4555-8555-555555555555'
  AND retired_at IS NULL;

COMMENT ON TABLE content_template_replicas IS '云端内容模版的只读副本：工艺、工程都是多份，按身份加修订各自一行';
COMMENT ON COLUMN content_template_replicas.name IS '模版名称；与云端相同';

ALTER TABLE assets
    ADD COLUMN template_id TEXT; -- 工艺所属模版身份；空表示还没按正文判过

-- 厂库正文是信封，SQL 里判不了：工程直接置空串，工艺先留空，领到租约后解开正文补上。
UPDATE assets
SET template_id = ''
WHERE kind = 'project';

COMMENT ON COLUMN assets.template_id IS '工艺所属模版身份，随正文算：正文写了 templateId 用它，没写的包角归预置包角、其余归预置焊接参数；工程为空串；空表示还没按正文判过，领约后补上';

ALTER TABLE asset_replicas
    ADD COLUMN template_id TEXT; -- 与源相同；空表示还没按正文判过

UPDATE asset_replicas
SET template_id = ''
WHERE kind = 'project';

COMMENT ON COLUMN asset_replicas.template_id IS '工艺所属模版身份：按送达正文判，与源相同；工程为空串；空表示还没判过，领约后补上';
