-- 允许删除未被工程依赖的工艺/工程；仍被 deps 引用的由应用层拒绝。副本仍禁止物理删除。

DROP TRIGGER IF EXISTS assets_no_delete ON assets;
DROP FUNCTION IF EXISTS prevent_asset_delete();
