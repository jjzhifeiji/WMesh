// 示教器会话互斥：新登录只作废更早的令牌，并发登录不会互删成两张废票。
package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// 先后建 A、B 两条示教器会话，按并发交错的顺序各删一次，只留最新的 B。
func TestDeleteOtherAppSessionsKeepsNewest(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 会话一小时后才过期，验收期间都算有效。
	exp := time.Now().UTC().Add(time.Hour)

	// 建本人账号，会话要挂在他名下。
	op, err := s.CreatePerson(ctx, "op", "操作员", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建本人账号失败就停测。
		t.Fatalf("person: %v", err)
	}
	// 建另一个人，验只删本人的会话。
	other, err := s.CreatePerson(ctx, "op2", "别人", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 建另一个人失败就停测。
		t.Fatalf("other: %v", err)
	}
	// 别人先登一条示教器会话，比本人的都早。
	if _, err := s.CreateAppSession(ctx, other.ID, "hash-other", exp); err != nil {
		// 建别人的会话失败就停测。
		t.Fatalf("other app: %v", err)
	}
	// 本人第一次登录建出会话 A。
	if _, err := s.CreateAppSession(ctx, op.ID, "hash-a", exp); err != nil {
		// 建会话 A 失败就停测。
		t.Fatalf("app a: %v", err)
	}
	// 本人网页会话，示教器登录不该动它。
	if _, err := s.CreateSession(ctx, op.ID, "hash-web", exp); err != nil {
		// 建网页会话失败就停测。
		t.Fatalf("web: %v", err)
	}
	// 本人第二次登录建出会话 B，比 A 晚。
	if _, err := s.CreateAppSession(ctx, op.ID, "hash-b", exp); err != nil {
		// 建会话 B 失败就停测。
		t.Fatalf("app b: %v", err)
	}

	// 第一次登录收尾晚到，按 A 清旧会话。
	if err := s.DeleteOtherAppSessions(ctx, op.ID, "hash-a"); err != nil {
		// 按 A 清旧会话报错就停测。
		t.Fatalf("drop by a: %v", err)
	}
	// 更晚的 B 必须还在，被删掉就会两张都废。
	if _, err := s.SessionByTokenHash(ctx, "hash-b"); err != nil {
		// 新会话被旧登录删掉就停测。
		t.Fatalf("b after drop by a: %v", err)
	}
	// A 自己也还在，它只删比自己早的。
	if _, err := s.SessionByTokenHash(ctx, "hash-a"); err != nil {
		// 本次会话被自己删掉就停测。
		t.Fatalf("a after drop by a: %v", err)
	}

	// 第二次登录收尾，按 B 清旧会话。
	if err := s.DeleteOtherAppSessions(ctx, op.ID, "hash-b"); err != nil {
		// 按 B 清旧会话报错就停测。
		t.Fatalf("drop by b: %v", err)
	}
	// 更早的 A 应已删掉，还查得到就失败。
	if _, err := s.SessionByTokenHash(ctx, "hash-a"); !errors.Is(err, domain.ErrNotFound) {
		// 旧会话没被作废就停测。
		t.Fatalf("a after drop by b: %v", err)
	}
	// 最新的 B 必须留下。
	if _, err := s.SessionByTokenHash(ctx, "hash-b"); err != nil {
		// 最新会话被删就停测。
		t.Fatalf("b after drop by b: %v", err)
	}
	// 本人网页会话应当保留。
	if _, err := s.SessionByTokenHash(ctx, "hash-web"); err != nil {
		// 网页会话被一起删掉就停测。
		t.Fatalf("web after drop: %v", err)
	}
	// 别人的示教器会话虽然更早也应保留。
	if _, err := s.SessionByTokenHash(ctx, "hash-other"); err != nil {
		// 删到别人的会话就停测。
		t.Fatalf("other after drop: %v", err)
	}

	// 本次令牌已不在库里时一条不删，也不报错。
	if err := s.DeleteOtherAppSessions(ctx, op.ID, "hash-gone"); err != nil {
		// 找不到本次令牌就报错，登录会被白白打断。
		t.Fatalf("drop by missing: %v", err)
	}
	// 本次令牌不在时 B 仍须保留。
	if _, err := s.SessionByTokenHash(ctx, "hash-b"); err != nil {
		// 拿不存在的令牌清掉了 B 就停测。
		t.Fatalf("b after drop by missing: %v", err)
	}
}
