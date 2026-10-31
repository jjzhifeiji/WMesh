package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ClientLatestLogin 是某台设备最近一次示教器登录，随通道保活报给 WAN。
type ClientLatestLogin struct {
	ClientID       uuid.UUID // 设备稳定身份
	OccurredAt     time.Time // 服务端记下的登录时间
	AppVersion     int64     // 示教器 versionCode；0 表示没报
	AppVersionName string    // 示教器 versionName
}

// LatestClientLogins 按设备取最近一条已对上本机的登录现场；没对上设备的不算。
func (s *Store) LatestClientLogins(ctx context.Context) ([]ClientLatestLogin, error) {
	// 准备接住每台一行。
	var rows []ClientLatestLogin
	// 每台设备只取时间最新的一条。
	err := s.db.WithContext(ctx).Raw(`SELECT DISTINCT ON (client_id) client_id, occurred_at, app_version, app_version_name
		FROM person_login_logs WHERE client_id IS NOT NULL ORDER BY client_id, occurred_at DESC`).Scan(&rows).Error
	return rows, err
}
