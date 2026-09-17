-- 本厂可关 Client 本机袋 SQLCipher，便于现场用 Database Inspector；默认仍加密。
-- 关加密后盘上是明文 SQLite；切换会让 Client 丢掉旧袋文件再重建。

ALTER TABLE factory_settings
    ADD COLUMN encrypt_pouch BOOLEAN NOT NULL DEFAULT true; -- 本机袋是否 SQLCipher 整库加密

COMMENT ON TABLE factory_settings IS '本厂一份 Client 策略：缓存、解封钥落盘与时效、本机袋是否加密；对本厂全部 Client';
COMMENT ON COLUMN factory_settings.encrypt_pouch IS '本机袋是否 SQLCipher 整库加密；false 时平板可用 Database Inspector';
