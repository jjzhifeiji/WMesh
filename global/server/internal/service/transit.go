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
	// 这一步失败就停，避免留下半截。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	defer contentcrypt.Zero(key) // 用完清租约材料。
	// 先复制快照，封成员时不改调用方手里的。
	out := snap
	// 把这一项接进结果。
	out.Members = append([]ClosureMember(nil), snap.Members...)
	// 逐个成员换上信封，库内明文保持不动。
	for i, m := range out.Members {
		// 每条成员当场造过站 DEK。
		env, err := contentcrypt.Seal(key, m.Content, contentcrypt.TransitAAD(factoryID, m.ID, m.Revision))
		// 封不上就拒绝下发，明文不能出站。
		if err != nil {
			return ClosureSnapshot{}, err
		}
		// 成员换成过站信封，库里的明文不动。
		out.Members[i].Content = env
	}
	return out, nil
}

// OpenSnapshotTransit 升档快照到站解开；无信封则当明文夹具。
func (s *Assets) OpenSnapshotTransit(ctx context.Context, snap AssetSnapshot) ([]byte, error) {
	// 取来源厂当前 L；没有则只接受明文夹具。
	key, err := s.store.LeaseKey(ctx, snap.SourceFactoryID)
	// 这一步失败就停，避免留下半截。
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
