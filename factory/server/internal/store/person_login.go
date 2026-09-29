package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
)

const (
	LoginKindPad    = "pad"    // 厂网示教器登录
	LoginKindClient = "client" // 已钉设备号的本机登录
	LoginKindMQTT   = "mqtt"   // 本厂 MQTT 回连补现场快照
	loginLogKeep    = 200      // 每人最多留下这么多条，再新的仍写入并裁掉更旧的
)

// PersonLoginLog 是一次示教器登录的现场快照，不含密码或令牌。
type PersonLoginLog struct {
	ID                 uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`      // 登录记录稳定身份
	PersonID           uuid.UUID  `gorm:"type:uuid;not null" json:"personId"`  // 本厂登录人
	Kind               string     `gorm:"not null" json:"kind"`                // pad / client / mqtt
	OccurredAt         time.Time  `gorm:"not null" json:"occurredAt"`          // 服务端记下的时间
	AppVersion         int64      `json:"appVersion"`                          // 示教器 versionCode；0 表示没报
	AppVersionName     string     `json:"appVersionName"`                      // 示教器 versionName
	DeviceSerial       string     `json:"deviceSerial"`                        // 机械臂识别号；未对臂可空
	DeviceModel        string     `json:"deviceModel"`                         // 平板型号
	DeviceManufacturer string     `json:"deviceManufacturer"`                  // 平板厂商
	AndroidRelease     string     `json:"androidRelease"`                      // 平板系统版本
	NetworkName        string     `json:"networkName"`                         // 当时 WiFi 名
	ClientID           *uuid.UUID `gorm:"type:uuid" json:"clientId,omitempty"` // 已匹配本机；未对臂为空
	ClientName         string     `json:"clientName"`                          // 当时设备名快照；未对臂为空
}

// 指定落库表名，避免查询时按类型名去猜。
func (PersonLoginLog) TableName() string { return "person_login_logs" }

// LoginSnap 是示教器自报的现场；空字段表示当时读不到。
type LoginSnap struct {
	Kind               string     // pad / client / mqtt
	AppVersion         int64      // versionCode
	AppVersionName     string     // versionName
	DeviceSerial       string     // 机械臂识别号
	DeviceModel        string     // 平板型号
	DeviceManufacturer string     // 平板厂商
	AndroidRelease     string     // 系统版本
	NetworkName        string     // WiFi 名
	ClientID           *uuid.UUID // 已匹配本机
	ClientName         string     // 本厂设备名
}

// InsertPersonLogin 写下这一次现场；每人只留最近若干条。
func (s *Store) InsertPersonLogin(ctx context.Context, personID uuid.UUID, snap LoginSnap) (PersonLoginLog, error) {
	// 人必须已经在本厂，否则不继续写。
	if err := s.assertPersonExists(ctx, personID); err != nil {
		return PersonLoginLog{}, err
	}
	// 去掉首尾空白，纯空白不当有效内容。
	kind := strings.TrimSpace(snap.Kind)
	// 去掉空白这一支不成立就换路。
	if kind == "" {
		// 没标来源就当厂网示教器登录。
		kind = LoginKindPad
	}
	// 组装这一次登录现场，不含密码或令牌。
	row := PersonLoginLog{
		ID:                 id.New(),
		PersonID:           personID,
		Kind:               kind,
		OccurredAt:         time.Now().UTC(),
		AppVersion:         snap.AppVersion,
		AppVersionName:     clipLoginText(snap.AppVersionName, 80),
		DeviceSerial:       clipLoginText(snap.DeviceSerial, 80),
		DeviceModel:        clipLoginText(snap.DeviceModel, 80),
		DeviceManufacturer: clipLoginText(snap.DeviceManufacturer, 80),
		AndroidRelease:     clipLoginText(snap.AndroidRelease, 40),
		NetworkName:        clipLoginText(snap.NetworkName, 80),
		ClientID:           snap.ClientID,
		ClientName:         clipLoginText(snap.ClientName, 80),
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return PersonLoginLog{}, err
	}
	// 只留最近若干条，避免无限长。
	var extra []PersonLoginLog
	// 加上筛选失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Where("person_id = ?", personID).
		Order("occurred_at DESC").Offset(loginLogKeep).Find(&extra).Error; err != nil {
		return row, err
	}
	// 超出保留条数的旧现场要删掉。
	if len(extra) > 0 {
		// 按需要预留位置，避免后面反复扩容。
		ids := make([]uuid.UUID, 0, len(extra))
		// 逐条处理，避免漏掉还要读或还要写的行。
		for _, e := range extra {
			// 收进结果，保持原来的先后顺序。
			ids = append(ids, e.ID)
		}
		// 删除匹配行失败就停，避免带着错误继续。
		if err := s.db.WithContext(ctx).Where("id IN ?", ids).Delete(&PersonLoginLog{}).Error; err != nil {
			return row, err
		}
	}
	return row, nil
}

// LatestPersonLogin 取该人最近一条登录快照；没有则 not found。
func (s *Store) LatestPersonLogin(ctx context.Context, personID uuid.UUID) (PersonLoginLog, error) {
	// 准备承接查到的登录现场。
	var row PersonLoginLog
	// 按条件去读，没有行交给后面的分支。
	err := s.db.WithContext(ctx).Where("person_id = ?", personID).Order("occurred_at DESC").First(&row).Error
	// 按条件取一行失败就停，避免带着错误继续。
	if err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PersonLoginLog{}, domain.ErrNotFound
		}
		return PersonLoginLog{}, err
	}
	return row, nil
}

// ListPersonLogins 按时间从新到旧列出该人登录快照。
func (s *Store) ListPersonLogins(ctx context.Context, personID uuid.UUID) ([]PersonLoginLog, error) {
	// 人必须已经在本厂，否则不继续写。
	if err := s.assertPersonExists(ctx, personID); err != nil {
		return nil, err
	}
	// 准备承接查到的多条登录现场。
	var rows []PersonLoginLog
	// 先加上筛选，结果留给紧跟着的判断。
	err := s.db.WithContext(ctx).Where("person_id = ?", personID).
		Order("occurred_at DESC").Limit(loginLogKeep).Find(&rows).Error
	// 按条件取多行失败就停，避免带着错误继续。
	if err != nil {
		return nil, err
	}
	// 没有行也交回空列表，避免调用方拿到空指针。
	if rows == nil {
		// 没有登录记录也交回空列表。
		rows = []PersonLoginLog{}
	}
	return rows, nil
}

// LatestLoginDevices 每人最近一次带上设备的登录现场。
func (s *Store) LatestLoginDevices(ctx context.Context) (map[uuid.UUID]PersonLoginLog, error) {
	// 准备承接查到的多条登录现场。
	var rows []PersonLoginLog
	// 先走手写查询，结果留给紧跟着的判断。
	err := s.db.WithContext(ctx).Raw(`
SELECT DISTINCT ON (person_id) *
FROM person_login_logs
WHERE client_name <> '' OR device_serial <> '' OR client_id IS NOT NULL
ORDER BY person_id, occurred_at DESC
`).Scan(&rows).Error
	// 扫描查询结果失败就停，避免带着错误继续。
	if err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make(map[uuid.UUID]PersonLoginLog, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, row := range rows {
		// 按身份放进映射，调用方好查找。
		out[row.PersonID] = row
	}
	return out, nil
}

// SameLoginSnap 现场没变，回连不必再写一条。
func SameLoginSnap(prev PersonLoginLog, snap LoginSnap) bool {
	// 先留空，对上本机再填身份文本。
	cid := ""
	if prev.ClientID != nil {
		// 记下对上的本机身份，用来判断现场变没变。
		cid = prev.ClientID.String()
	}
	// 先留空，对上本机再填身份文本。
	want := ""
	if snap.ClientID != nil {
		// 记下对上的本机身份，用来判断现场变没变。
		want = snap.ClientID.String()
	}
	return prev.AppVersion == snap.AppVersion &&
		prev.AppVersionName == clipLoginText(snap.AppVersionName, 80) &&
		prev.DeviceSerial == clipLoginText(snap.DeviceSerial, 80) &&
		prev.DeviceModel == clipLoginText(snap.DeviceModel, 80) &&
		prev.DeviceManufacturer == clipLoginText(snap.DeviceManufacturer, 80) &&
		prev.AndroidRelease == clipLoginText(snap.AndroidRelease, 40) &&
		prev.NetworkName == clipLoginText(snap.NetworkName, 80) &&
		prev.ClientName == clipLoginText(snap.ClientName, 80) &&
		cid == want
}

// 现场字符串截到上限，避免把整段扫描结果塞进来。
func clipLoginText(s string, max int) string {
	// 去掉首尾空白，纯空白不当有效内容。
	s = strings.TrimSpace(s)
	// 超过字数上限就截断，避免超出列宽。
	if max < 1 || utf8.RuneCountInString(s) <= max {
		return s
	}
	// 按字截到上限，避免超长内容进库。
	return string([]rune(s)[:max])
}
