-- 去掉组织类型词典：节点直接挂工厂或上级；已落库路径快照原文不改。

ALTER TABLE org_units DROP COLUMN org_type_id;
DROP TABLE org_types;
