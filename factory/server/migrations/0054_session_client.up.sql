-- 示教器会话钉设备（D-59）：登录或登录后第一次对上臂时记下这台 Client，MQTT CONNECT 只认这一台；换设备须重新登录。
-- +goose Up

ALTER TABLE sessions
    ADD COLUMN client_id UUID; -- 本次示教器会话钉住的 Client；空表示还没对上臂

COMMENT ON COLUMN sessions.client_id IS '本次示教器会话钉住的 Client；空表示还没对上臂，此时 MQTT 只许用本人身份连；一经钉住不改，换设备须重新登录';

-- 升级前已登录的示教器会话还空着：按本次会话建立之后最近一条对上臂的登录记录补钉，
-- 免得升级后在线示教器 MQTT 一直被拒（示教器同一现场不再补报，要重启或重登才好）。
-- 只认 pad/client 两种记录，且记录里的臂号就是那台未作废设备的号；mqtt 回连记录按 CONNECT 自报身份记，不拿来钉。
UPDATE sessions AS s
SET client_id = (
    SELECT l.client_id
    FROM person_login_logs AS l
    JOIN clients AS c ON c.id = l.client_id
    WHERE l.person_id = s.person_id
      AND l.kind IN ('pad', 'client')
      AND l.occurred_at >= s.created_at
      AND c.status = 'bound'
      AND c.device_serial <> ''
      AND upper(c.device_serial) = upper(btrim(l.device_serial))
    ORDER BY l.occurred_at DESC
    LIMIT 1
)
WHERE s.kind = 'app'
  AND s.client_id IS NULL
  AND s.expires_at > now();
