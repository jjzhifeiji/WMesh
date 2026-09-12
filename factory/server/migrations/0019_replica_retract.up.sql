-- 云端删除后厂端撤回展示；历史修订仍可被已钉依赖命中。

ALTER TABLE asset_replicas
    ADD COLUMN retracted BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN asset_replicas.retracted IS '云端已删除；列表不再展示，已钉依赖仍可读该修订';
