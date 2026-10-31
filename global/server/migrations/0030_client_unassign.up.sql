-- 设备可收回到未分配：收回时修订照样升高，厂端靠更高修订作废旧绑定，之后再分配也只向前。
-- 未分配不再要求修订为 0，只要求没有工厂、没有生效时间。
-- +goose Up

ALTER TABLE clients DROP CONSTRAINT IF EXISTS clients_check;

ALTER TABLE clients ADD CONSTRAINT clients_binding_check CHECK (
    (factory_id IS NULL AND binding_revision >= 0 AND bound_at IS NULL)
    OR (factory_id IS NOT NULL AND binding_revision >= 1 AND bound_at IS NOT NULL)
); -- 未分配无生效时间；已分配修订至少为 1

COMMENT ON COLUMN clients.binding_revision IS '绑定修订；从未分配为 0，分配、改分、收回都必须升高';
