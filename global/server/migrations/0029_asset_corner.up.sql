-- 工艺记下是不是包角工艺：示教器按正文顶层 kind 为 cornerGeometry 分开包角和焊接参数，网页列表不带正文，要靠这一列分开选。
-- 已有行先置否；工艺按正文里的 kind 补上，之后每次写正文随正文重算。
-- +goose Up

ALTER TABLE assets
    ADD COLUMN corner BOOLEAN NOT NULL DEFAULT false; -- 包角工艺

UPDATE assets
SET corner = true
WHERE kind = 'process'
  AND convert_from(content, 'UTF8') ~ '"kind"\s*:\s*"cornerGeometry"';

COMMENT ON COLUMN assets.corner IS '包角工艺：正文顶层 kind 为 cornerGeometry；工程恒为否';
