package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/clientmqtt"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

// NoteAppPresence 记下示教器版本和最近见到；无版本只刷新时间。
func (s *Node) NoteAppPresence(ctx context.Context, personID uuid.UUID, version int64, versionName string) error {
	// 没有人员身份则跳过，避免记到空人
	if personID == uuid.Nil {
		return nil
	}
	// 刷新示教器版本和最近见到，再交回调用方
	return s.store.NotePersonApp(ctx, personID, version, versionName)
}

// RecordAppLogin 记下这一次示教器登录现场；MQTT 回连现场没变则跳过。
func (s *Node) RecordAppLogin(ctx context.Context, personID uuid.UUID, snap LoginSnap) error {
	// 没有人员身份则跳过，避免记到空人
	if personID == uuid.Nil {
		return nil
	}
	// 用本厂名录补设备名和识别号
	s.fillLoginDevice(ctx, &snap)
	// 回连现场没变可以跳过，其它登录照记
	if snap.Kind == LoginKindMQTT {
		// 取上次登录现场，没变则不必再插
		prev, err := s.store.LatestPersonLogin(ctx, personID)
		// 没有错误才采用这次结果，失败另走拒绝
		if err == nil && store.SameLoginSnap(prev, snap) {
			// 刷新示教器版本和最近见到，再交回调用方
			return s.store.NotePersonApp(ctx, personID, snap.AppVersion, snap.AppVersionName)
		}
		// 不是没有记录，读取真失败必须停住
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	// 现场写不进去，不更新最近见到
	if _, err := s.store.InsertPersonLogin(ctx, personID, snap); err != nil {
		return err
	}
	// 刷新示教器版本和最近见到，再交回调用方
	return s.store.NotePersonApp(ctx, personID, snap.AppVersion, snap.AppVersionName)
}

// HandleAppUp 认示教器 presence；正文或其它 typ 交给闭包回执。
func (s *Node) HandleAppUp(ctx context.Context, personID uuid.UUID, payload []byte) {
	// 没有人员身份则跳过，避免记到空人
	if personID == uuid.Nil || clientmqtt.HasBody(payload) {
		return
	}
	// 准备接在场回报，种类不对就忽略这条上行
	var cmd struct {
		Typ                string `json:"typ"`                // 上行种类，只认在场回报
		Version            int64  `json:"version"`            // 示教器版本号，没有则只刷新时间
		VersionName        string `json:"versionName"`        // 示教器版本名，可以空着
		DeviceSerial       string `json:"deviceSerial"`       // 机械臂识别号，回连时补现场
		DeviceModel        string `json:"deviceModel"`        // 设备型号，只给人看
		DeviceManufacturer string `json:"deviceManufacturer"` // 设备厂商，只给人看
		AndroidRelease     string `json:"androidRelease"`     // 系统版本，方便排障
		NetworkName        string `json:"networkName"`        // 当时连接的网络名
		ClientID           string `json:"clientId"`           // 本机身份，用来对上这台设备
	}
	// 种类不对则忽略，不当成在场或回执
	if json.Unmarshal(payload, &cmd) != nil || cmd.Typ != clientmqtt.TypPresence {
		return
	}
	// 组这次回连现场，没变则不必再插历史
	snap := LoginSnap{
		Kind:               LoginKindMQTT,
		AppVersion:         cmd.Version,
		AppVersionName:     cmd.VersionName,
		DeviceSerial:       cmd.DeviceSerial,
		DeviceModel:        cmd.DeviceModel,
		DeviceManufacturer: cmd.DeviceManufacturer,
		AndroidRelease:     cmd.AndroidRelease,
		NetworkName:        cmd.NetworkName,
	}
	// 没有错误才采用这次结果，失败另走拒绝
	if id, err := uuid.Parse(cmd.ClientID); err == nil {
		// 补上本机身份，现场才能对上设备
		snap.ClientID = &id
	}
	// 记下示教器登录，回连没变则跳过，失败不打断当前返回
	_ = s.RecordAppLogin(ctx, personID, snap)
}

// fillLoginDevice 用本厂名录补设备名和识别号；人身份不当设备。
func (s *Node) fillLoginDevice(ctx context.Context, snap *LoginSnap) {
	// 没有现场则不补设备名和识别号
	if snap == nil {
		return
	}
	// 有创建人或本机才拷走指针，空的保持空
	if snap.ClientID != nil {
		// 按身份读本厂这台设备
		cl, err := s.store.ClientByID(ctx, *snap.ClientID)
		// 没有错误才采用这次结果，失败另走拒绝
		if err == nil {
			// 现场没带设备名或识别号，才用名录补
			if snap.ClientName == "" {
				// 用名录里的设备名，人的身份不当设备
				snap.ClientName = cl.Name
			}
			// 名字或识别号为空则拒绝或去名录补
			if strings.TrimSpace(snap.DeviceSerial) == "" {
				// 补上已钉的识别号，空号不能当已登记
				snap.DeviceSerial = cl.DeviceSerial
			}
			return
		}
	}
	// 去掉首尾空白后再做判断
	serial := strings.TrimSpace(snap.DeviceSerial)
	// 空识别号拒绝，登记和登录都必须读到号
	if serial == "" {
		return
	}
	// 按机械臂号找未作废设备
	cl, err := s.store.BoundClientByDeviceSerial(ctx, serial)
	// 号对不上未作废设备，拒绝登录
	if err != nil {
		return
	}
	// 另拷一份身份再取址，避免下一轮把指针改掉
	id := cl.ID
	// 补上本机身份，现场才能对上设备
	snap.ClientID = &id
	// 现场没带设备名或识别号，才用名录补
	if snap.ClientName == "" {
		// 用名录里的设备名，人的身份不当设备
		snap.ClientName = cl.Name
	}
	// 名字或识别号为空则拒绝或去名录补
	if strings.TrimSpace(snap.DeviceSerial) == "" {
		// 补上已钉的识别号，空号不能当已登记
		snap.DeviceSerial = cl.DeviceSerial
	}
}

// RecordOwnAppLogin 本会话人员补记一次现场（匹配设备号后回厂网）。
func (s *Node) RecordOwnAppLogin(ctx context.Context, token string, snap LoginSnap) error {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return err
	}
	// 不是工程闭包则拒绝装入或下发
	if snap.Kind == "" {
		// 标成厂网或回连，两种登录分开记
		snap.Kind = LoginKindPad
	}
	// 记下示教器登录，回连没变则跳过，再交回调用方
	return s.RecordAppLogin(ctx, acc.ID, snap)
}

// NoteAppMQTT 示教器 MQTT 连上或断开；同一人多条连接要都断才离线。
func (s *Node) NoteAppMQTT(ctx context.Context, personID uuid.UUID, online bool) {
	// 按回连增减在线人数，大于零才算在线
	s.noteAppMQTT(ctx, personID, online)
}

// noteAppMQTT 按连接数计在线；连上顺带刷新最后见到。
func (s *kernel) noteAppMQTT(ctx context.Context, personID uuid.UUID, online bool) {
	// 没有人员身份则跳过，避免记到空人
	if personID == uuid.Nil {
		return
	}
	// 先锁住共享计数或同版本拉包
	s.mqttMu.Lock()
	// 在线表还没建就先建，避免空表计数出错
	if s.mqttOnline == nil {
		// 在线表还没有就先建，避免空表写入崩掉
		s.mqttOnline = map[uuid.UUID]int{}
	}
	// 离线标记决定能不能向厂端拉新包
	if online {
		// 这人又连上一条回连，计数大于零才算在线
		s.mqttOnline[personID]++
		// 改完立刻放锁，避免留下不一致的结果
		s.mqttMu.Unlock()
		// 刷新示教器版本和最近见到，失败不打断当前返回
		_ = s.store.NotePersonApp(ctx, personID, 0, "")
		return
	}
	// 先算减完还剩几条连接，到零才算离线
	n := s.mqttOnline[personID] - 1
	// 条数超过上限就停，避免把袋或报表撑满
	if n <= 0 {
		// 从集合里撤掉这一份
		delete(s.mqttOnline, personID)
		// 其余情况走这里，避免前面的分支漏判
	} else {
		// 减掉一条回连，到零就从在线表拿掉
		s.mqttOnline[personID] = n
	}
	// 改完立刻放锁，避免留下不一致的结果
	s.mqttMu.Unlock()
}

// appMQTTPersonIDs 当前 MQTT 连着的本厂人员。
func (s *kernel) appMQTTPersonIDs() map[uuid.UUID]struct{} {
	s.mqttMu.Lock()
	defer s.mqttMu.Unlock()
	out := make(map[uuid.UUID]struct{}, len(s.mqttOnline))
	for id, n := range s.mqttOnline {
		// 还有回连就保持在线，到零才算离开
		if n > 0 {
			// 记下仍被引用或仍在线的身份
			out[id] = struct{}{}
		}
	}
	return out
}
