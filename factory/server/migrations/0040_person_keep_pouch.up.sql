-- 每人退出后是否保留示教器上自己的库文件；默认留。

ALTER TABLE people
    ADD COLUMN keep_pouch BOOLEAN NOT NULL DEFAULT true; -- 退出后留下该人的库文件

COMMENT ON COLUMN people.keep_pouch IS '退出后是否保留该人在示教器上的库文件；默认留';
