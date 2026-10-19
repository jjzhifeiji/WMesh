-- 同一升档源只对应一条厂级：并发升档曾可能各插一条，之后再升只覆盖其中一条，另一条成孤儿。
-- 先把存量重复摘掉来源：同一 source_id 按 updated_at 倒序留最新一条带源（同刻按身份取小的，与按源回读一致），
-- 其余行的来源两列置空，变成独立厂级，不删数据；再建 source_id 非空的部分唯一索引。

WITH ranked AS (
    SELECT id, row_number() OVER (PARTITION BY source_id ORDER BY updated_at DESC, id) AS rn
    FROM assets
    WHERE source_id IS NOT NULL
)
UPDATE assets AS a
SET source_id = NULL, source_revision = NULL
FROM ranked AS r
WHERE a.id = r.id AND r.rn > 1;

CREATE UNIQUE INDEX assets_source_id_uq ON assets (source_id) WHERE source_id IS NOT NULL; -- 同一升档源只一条厂级

COMMENT ON INDEX assets_source_id_uq IS '同一升档源只对应一条厂级；并发升档撞上时回读已有行按摘要跳过或覆盖';
