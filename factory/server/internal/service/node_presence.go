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
	if personID == uuid.Nil {
		return nil
	}
	return s.store.NotePersonApp(ctx, personID, version, versionName)
}

// RecordAppLogin 记下这一次示教器登录现场；MQTT 回连现场没变则跳过。
func (s *Node) RecordAppLogin(ctx context.Context, personID uuid.UUID, snap LoginSnap) error {
	if personID == uuid.Nil {
		return nil
	}
	s.fillLoginDevice(ctx, &snap)
	if snap.Kind == LoginKindMQTT {
		prev, err := s.store.LatestPersonLogin(ctx, personID)
		if err == nil && store.SameLoginSnap(prev, snap) {
			return s.store.NotePersonApp(ctx, personID, snap.AppVersion, snap.AppVersionName)
		}
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	if _, err := s.store.InsertPersonLogin(ctx, personID, snap); err != nil {
		return err
	}
	return s.store.NotePersonApp(ctx, personID, snap.AppVersion, snap.AppVersionName)
}

// HandleAppUp 认示教器 presence；正文或其它 typ 交给闭包回执。
func (s *Node) HandleAppUp(ctx context.Context, personID uuid.UUID, payload []byte) {
	if personID == uuid.Nil || clientmqtt.HasBody(payload) {
		return
	}
	var cmd struct {
		Typ                string `json:"typ"`
		Version            int64  `json:"version"`
		VersionName        string `json:"versionName"`
		DeviceSerial       string `json:"deviceSerial"`
		DeviceModel        string `json:"deviceModel"`
		DeviceManufacturer string `json:"deviceManufacturer"`
		AndroidRelease     string `json:"androidRelease"`
		NetworkName        string `json:"networkName"`
		ClientID           string `json:"clientId"`
	}
	if json.Unmarshal(payload, &cmd) != nil || cmd.Typ != clientmqtt.TypPresence {
		return
	}
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
	if id, err := uuid.Parse(cmd.ClientID); err == nil {
		snap.ClientID = &id
	}
	_ = s.RecordAppLogin(ctx, personID, snap)
}

// fillLoginDevice 用本厂名录补设备名和识别号；人身份不当设备。
func (s *Node) fillLoginDevice(ctx context.Context, snap *LoginSnap) {
	if snap == nil {
		return
	}
	if snap.ClientID != nil {
		cl, err := s.store.ClientByID(ctx, *snap.ClientID)
		if err == nil {
			if snap.ClientName == "" {
				snap.ClientName = cl.Name
			}
			if strings.TrimSpace(snap.DeviceSerial) == "" {
				snap.DeviceSerial = cl.DeviceSerial
			}
			return
		}
	}
	serial := strings.TrimSpace(snap.DeviceSerial)
	if serial == "" {
		return
	}
	cl, err := s.store.BoundClientByDeviceSerial(ctx, serial)
	if err != nil {
		return
	}
	id := cl.ID
	snap.ClientID = &id
	if snap.ClientName == "" {
		snap.ClientName = cl.Name
	}
	if strings.TrimSpace(snap.DeviceSerial) == "" {
		snap.DeviceSerial = cl.DeviceSerial
	}
}

// RecordOwnAppLogin 本会话人员补记一次现场（匹配设备号后回厂网）。
func (s *Node) RecordOwnAppLogin(ctx context.Context, token string, snap LoginSnap) error {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return err
	}
	if snap.Kind == "" {
		snap.Kind = LoginKindPad
	}
	return s.RecordAppLogin(ctx, acc.ID, snap)
}

// NoteAppMQTT 示教器 MQTT 连上或断开；同一人多条连接要都断才离线。
func (s *Node) NoteAppMQTT(ctx context.Context, personID uuid.UUID, online bool) {
	s.noteAppMQTT(ctx, personID, online)
}

// noteAppMQTT 按连接数计在线；连上顺带刷新最后见到。
func (s *kernel) noteAppMQTT(ctx context.Context, personID uuid.UUID, online bool) {
	if personID == uuid.Nil {
		return
	}
	s.mqttMu.Lock()
	if s.mqttOnline == nil {
		s.mqttOnline = map[uuid.UUID]int{}
	}
	if online {
		s.mqttOnline[personID]++
		s.mqttMu.Unlock()
		_ = s.store.NotePersonApp(ctx, personID, 0, "")
		return
	}
	n := s.mqttOnline[personID] - 1
	if n <= 0 {
		delete(s.mqttOnline, personID)
	} else {
		s.mqttOnline[personID] = n
	}
	s.mqttMu.Unlock()
}

// appMQTTPersonIDs 当前 MQTT 连着的本厂人员。
func (s *kernel) appMQTTPersonIDs() map[uuid.UUID]struct{} {
	s.mqttMu.Lock()
	defer s.mqttMu.Unlock()
	out := make(map[uuid.UUID]struct{}, len(s.mqttOnline))
	for id, n := range s.mqttOnline {
		if n > 0 {
			out[id] = struct{}{}
		}
	}
	return out
}

