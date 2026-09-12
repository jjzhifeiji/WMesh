-- 每人最多一条有效组织分配；多出来的保留最早一条，其余标结束。

UPDATE assignments AS a
SET status = 'ended', ended_at = now()
WHERE a.status = 'active'
  AND EXISTS (
    SELECT 1 FROM assignments AS b
    WHERE b.person_id = a.person_id
      AND b.status = 'active'
      AND (b.created_at < a.created_at OR (b.created_at = a.created_at AND b.id < a.id))
  );

DROP INDEX IF EXISTS assignments_one_active;
CREATE UNIQUE INDEX assignments_one_active_person ON assignments (person_id) WHERE status = 'active'; -- 每人最多一个有效节点

COMMENT ON TABLE assignments IS '人员到组织节点的关系；每人最多一条有效分配，取消只改状态，不删行';
COMMENT ON COLUMN assignments.person_id IS '本厂人员；有效分配每人最多一条';
