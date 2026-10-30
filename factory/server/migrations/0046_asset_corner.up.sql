-- 工艺记下是不是包角工艺：示教器按正文顶层 kind 为 cornerGeometry 分开包角和焊接参数，网页列表不带正文，要靠这一列分开选。
-- 厂库正文是信封，SQL 里判不了：工程直接置否，工艺先留空，厂端领到租约后解开正文补上；之后每次写正文随正文重算。
-- +goose Up

ALTER TABLE assets
    ADD COLUMN corner BOOLEAN; -- 包角工艺；空表示还没按正文判过

UPDATE assets
SET corner = false
WHERE kind = 'project';

COMMENT ON COLUMN assets.corner IS '包角工艺：正文顶层 kind 为 cornerGeometry；工程恒为否；空表示还没按正文判过，领约后补上';

ALTER TABLE asset_replicas
    ADD COLUMN corner BOOLEAN; -- 与源相同；空表示还没按正文判过

UPDATE asset_replicas
SET corner = false
WHERE kind = 'project';

COMMENT ON COLUMN asset_replicas.corner IS '包角工艺：按送达正文判，与源相同；工程恒为否；空表示还没判过，领约后补上';
