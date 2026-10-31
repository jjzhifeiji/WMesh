-- 升档改为每次复制一份新的厂级草稿，同一源可有多份，去掉同源唯一。
-- 厂端建的工艺工程都可复制，保密只对平台级副本，存量一并放开。
-- +goose Up

DROP INDEX IF EXISTS assets_source_id_uq;

UPDATE assets SET copyable = true WHERE NOT copyable;

COMMENT ON COLUMN assets.source_id IS '升档来源的个人级身份，只留出处；同一源可升出多份';
