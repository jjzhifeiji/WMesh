-- 名录在线按 MQTT 会话计；空表示离线。

COMMENT ON COLUMN factories.channel_connected_at IS '当前 MQTT 会话连上的时间；空表示离线';
COMMENT ON COLUMN factories.channel_last_seen_at IS '最近一次 MQTT 保活';
