package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/secret"
)

// 示教器会话与设备绑定（D-59）：登录时带的臂号、或登录后第一次对上臂补报的臂号，
// 解析成本厂一台未作废 Client 后钉进这条会话；MQTT CONNECT 报的已登记 clientId 必须等于它。
// 钉住后不改：同一个人换到别的设备要重新登录，旧令牌随新登录作废。

// padClockSkew 是补报原时刻（示教器本机钟）与会话建立时刻（厂端钟）比先后时容让的钟差：
// 平板钟慢几分钟时，本次登录后臂网上攒的补报原时刻会略早于会话建立，不能因此钉不上（P3-R1）；
// 更早那次登录的补报通常早得多，仍按「早于会话」不钉。
const padClockSkew = 10 * time.Minute

// BindPadClient 按示教器自报的臂号把本次会话钉到一台 Client；还没钉过才钉。
// 臂号为空、对不上本厂未作废设备、自报 clientId 与臂号对不上，或是更早那次登录的补报，都不钉也不报错；
// 已钉在别的设备上时交回 ErrSessionClientMismatch，示教器据此提示换设备须重新登录。
func (s *Node) BindPadClient(ctx context.Context, token string, snap LoginSnap) error {
	// 按臂号找本厂未作废设备，找不到就不钉
	cl, ok, err := s.padClientOf(ctx, snap)
	// 名录读失败照实交回，示教器下一轮再补报
	if err != nil || !ok {
		return err
	}
	// 取本次会话，补报的原时刻早于它就是上一次登录的现场，不拿来钉这一次
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	// 会话读不到（过期、作废）就不钉，鉴权由调用方另做
	if err != nil {
		return nil
	}
	// 更早那次登录的补报不钉本次会话；原时刻在会话建立前钟差容让之内的按本次算
	if snap.OccurredAt != nil && snap.OccurredAt.Before(sess.CreatedAt.Add(-padClockSkew)) {
		return nil
	}
	// 只在还空着时写，交回最终钉的那台
	bound, err := s.store.BindSessionClient(ctx, sess.TokenHash, cl.ID)
	// 网页会话不钉设备，按没钉处理
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	// 写库失败照实交回
	if err != nil {
		return err
	}
	// 已钉在别的设备上，这次不改，交回冲突
	if bound != cl.ID {
		return domain.ErrSessionClientMismatch
	}
	return nil
}

// padClientOf 由自报现场解析出这台 Client：只认臂号对上的未作废设备；自报 clientId 和臂号对不上就不认。
func (s *Node) padClientOf(ctx context.Context, snap LoginSnap) (Client, bool, error) {
	// 去掉首尾空白，没读到臂号就不钉
	serial := strings.TrimSpace(snap.DeviceSerial)
	// 没有臂号不钉，只报 clientId 证明不了在这台臂上
	if serial == "" {
		return Client{}, false, nil
	}
	// 按机械臂号找未作废设备
	cl, err := s.store.BoundClientByDeviceSerial(ctx, serial)
	// 号对不上本厂设备就不钉
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrDeviceSerialRequired) {
		return Client{}, false, nil
	}
	// 名录读失败照实交回
	if err != nil {
		return Client{}, false, err
	}
	// 自报的 clientId 和臂号对上的不是同一台，自相矛盾就不钉
	if snap.ClientID != nil && *snap.ClientID != cl.ID {
		return Client{}, false, nil
	}
	return cl, true, nil
}

// sessionClient 读这条令牌的会话钉住的 Client；没钉或读不到交回空。
func (s *Node) sessionClient(ctx context.Context, token string) uuid.UUID {
	// 按令牌摘要取会话，原文不入库
	sess, err := s.store.SessionByTokenHash(ctx, secret.TokenHash(token))
	// 读不到或还没钉就当没有
	if err != nil || sess.ClientID == nil {
		return uuid.Nil
	}
	return *sess.ClientID
}
