package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/contentcrypt"
)

// ApplyContentLease 把 WAN 租约钥放进内存并解开 MK。
func (s *Service) ApplyContentLease(ctx context.Context, key []byte, notAfter time.Time) error {
	// 钥只进本厂进程内存，不写审计。
	return s.store.ApplyContentLease(ctx, key, notAfter)
}

// openTransitMembers 解开过站成员正文；无 WM2 前缀的夹具明文不绑租约。
func (s *kernel) openTransitMembers(snap ClosureSnapshot) (ClosureSnapshot, error) {
	out := snap
	out.Members = append([]ClosureMember(nil), snap.Members...)
	needKey := false
	for _, m := range out.Members {
		if contentcrypt.IsEnvelope(m.Content) {
			needKey = true
			break
		}
	}
	var key []byte
	if needKey {
		var err error
		key, err = s.store.TransitKey()
		if err != nil {
			return ClosureSnapshot{}, err
		}
		defer contentcrypt.Zero(key)
	}
	fid := s.store.FactoryID()
	for i, m := range out.Members {
		if !contentcrypt.IsEnvelope(m.Content) {
			continue
		}
		body, err := contentcrypt.Open(key, m.Content, contentcrypt.TransitAAD(fid, m.ID, m.Revision))
		if err != nil {
			return ClosureSnapshot{}, err
		}
		out.Members[i].Content = body
	}
	return out, nil
}

// sealTransitContent 用当前 L 把明文封成过站信封。
func (s *kernel) sealTransitContent(assetID uuid.UUID, rev int64, content []byte) ([]byte, error) {
	// 过站另造 DEK，不复用厂库信封。
	key, err := s.store.TransitKey()
	if err != nil {
		return nil, err
	}
	defer contentcrypt.Zero(key)
	return contentcrypt.Seal(key, content, contentcrypt.TransitAAD(s.store.FactoryID(), assetID, rev))
}
