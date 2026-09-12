-- 建厂码哈希与初始超管显示名：厂端出站认领，WAN 不再反打厂内网。
-- 不存建厂码原文、日常密码或厂内人员明细。

ALTER TABLE factories
    ADD COLUMN enrollment_token_hash TEXT, -- 一次性建厂码哈希，认领成功后清空
    ADD COLUMN enrolled_at TIMESTAMPTZ; -- 厂端认领成功时间；未认领为空

COMMENT ON COLUMN factories.enrollment_token_hash IS '一次性建厂码哈希，认领成功后清空';
COMMENT ON COLUMN factories.enrolled_at IS '厂端认领成功时间；未认领为空';

ALTER TABLE initial_super_admins
    ADD COLUMN display_name TEXT NOT NULL DEFAULT ''; -- 交付时显示名，不是秘密

COMMENT ON COLUMN initial_super_admins.display_name IS '交付时显示名，不是秘密';
