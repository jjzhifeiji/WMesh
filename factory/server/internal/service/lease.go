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
	// 先复制快照，解开时不改库里的原文
	out := snap
	// 复制成员切片，解开时不改库里的原文
	out.Members = append([]ClosureMember(nil), snap.Members...)
	// 先假设都是明文，见到信封再去取钥
	needKey := false
	// 逐个成员解封或重封，缺一个就整包失败
	for _, m := range out.Members {
		// 是过站信封才去取钥，明文夹具保持原样
		if contentcrypt.IsEnvelope(m.Content) {
			// 有过站信封，后面必须取钥才能解开
			needKey = true
			break
		}
	}
	// 先留空钥，只有见到密文才去取
	var key []byte
	// 只有见到信封才取钥，全是明文就不用
	if needKey {
		// 信封才取过站钥，用完清零。
		var err error
		// 取过站钥，用完必须清零
		key, err = s.store.TransitKey()
		// 过站钥取不到，不能解或封正文
		if err != nil {
			return ClosureSnapshot{}, err
		}
		// 离开时清掉钥或放锁，避免秘密残留或堵住回连
		defer contentcrypt.Zero(key)
	}
	// 取出本厂稳定身份，封包和审计都要用
	fid := s.store.FactoryID()
	// 逐个成员解封或重封，缺一个就整包失败
	for i, m := range out.Members {
		// 是过站信封才去取钥，明文夹具保持原样
		if !contentcrypt.IsEnvelope(m.Content) {
			continue
		}
		// 绑上厂、成员和修订再解封
		body, err := contentcrypt.Open(key, m.Content, contentcrypt.TransitAAD(fid, m.ID, m.Revision))
		// 附加数据拼不齐，拒绝解封
		if err != nil {
			return ClosureSnapshot{}, err
		}
		// 换成解开的正文，后面按明文验收
		out.Members[i].Content = body
	}
	return out, nil
}

// sealTransitContent 用当前 L 把明文封成过站信封。
func (s *kernel) sealTransitContent(assetID uuid.UUID, rev int64, content []byte) ([]byte, error) {
	// 过站另造 DEK，不复用厂库信封。
	key, err := s.store.TransitKey()
	// 过站钥取不到，不能解或封正文
	if err != nil {
		return nil, err
	}
	// 离开时清掉钥或放锁，避免秘密残留或堵住回连
	defer contentcrypt.Zero(key)
	// 取出本厂稳定身份，封包和审计都要用，再交回调用方
	return contentcrypt.Seal(key, content, contentcrypt.TransitAAD(s.store.FactoryID(), assetID, rev))
}
