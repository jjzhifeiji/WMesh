-- 厂端通道在线监控：上线、最近心跳、离线时间。不存通道令牌或厂内人员。

ALTER TABLE factories
    ADD COLUMN channel_connected_at TIMESTAMPTZ, -- 当前这条 WSS 连上的时间；空表示离线
    ADD COLUMN channel_last_seen_at TIMESTAMPTZ, -- 最近一次心跳或握手
    ADD COLUMN channel_disconnected_at TIMESTAMPTZ; -- 最近一次断开；在线时为空

COMMENT ON COLUMN factories.channel_connected_at IS '当前这条 WSS 连上的时间；空表示离线';
COMMENT ON COLUMN factories.channel_last_seen_at IS '最近一次心跳或握手';
COMMENT ON COLUMN factories.channel_disconnected_at IS '最近一次断开；在线时为空';
