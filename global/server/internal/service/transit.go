package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/contentcrypt"
	"wmesh/global/internal/platform/domain"
)

// SealClosureTransit 下发前把成员正文封给该厂当前租约；库内仍是明文。
func (s *Closure) SealClosureTransit(ctx context.Context, factoryID uuid.UUID, snap ClosureSnapshot) (ClosureSnapshot, error) {
	// 用该厂当前 L 封成员正文；WAN 库仍是明文。
	key, err := s.store.LeaseKey(ctx, factoryID)
	if err != nil {
		return ClosureSnapshot{}, err
	}
	defer contentcrypt.Zero(key) // 用完清租约材料。
	out := snap
	out.Members = append([]ClosureMember(nil), snap.Members...)
	for i, m := range out.Members {
		// 每条成员当场造过站 DEK。
		env, err := contentcrypt.Seal(key, m.Content, contentcrypt.TransitAAD(factoryID, m.ID, m.Revision))
		if err != nil {
			return ClosureSnapshot{}, err
		}
		out.Members[i].Content = env
	}
	return out, nil
}

// OpenSnapshotTransit 升档快照到站解开；无信封则当明文夹具。
func (s *Assets) OpenSnapshotTransit(ctx context.Context, snap AssetSnapshot) ([]byte, error) {
	// 取来源厂当前 L；没有则只接受明文夹具。
	key, err := s.store.LeaseKey(ctx, snap.SourceFactoryID)
	if err != nil {
		// 有信封却没有 L，当损坏。
		if contentcrypt.IsEnvelope(snap.Content) {
			return nil, domain.ErrIntegrity
		}
		return snap.Content, nil
	}
	defer contentcrypt.Zero(key) // 用完清租约材料。
	// 无 WM2 前缀当明文夹具。
	return contentcrypt.Open(key, snap.Content, contentcrypt.TransitAAD(snap.SourceFactoryID, snap.SourceID, snap.SourceRevision))
}
