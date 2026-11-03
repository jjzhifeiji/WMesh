-- 焊汇总的日改为厂端时区日：WAN 不按 occurredAt 重新分桶，只存厂端算好的日键，两边按日报表一致。
-- +goose Up

COMMENT ON COLUMN weld_summaries.day IS '厂端按本厂时区（默认 Asia/Shanghai）算好的日历日，WAN 原样存放、不再按 UTC 重分';
