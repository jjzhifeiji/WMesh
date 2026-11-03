// G：登录连续失败到上限就锁。
package service_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 同一登录名同一来源连续失败五次就锁；锁住时口令对也拒绝；换来源不受影响；两条登录路共用计数。
func TestLoginFailLimit(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 这台来源的上下文。
	from1 := factory.WithClientIP(ctx, "10.0.0.1")
	// 另一台来源的上下文。
	from2 := factory.WithClientIP(ctx, "10.0.0.2")
	// 先在网页登录错四次，还没到上限。
	for i := 0; i < 4; i++ {
		// 错口令应按口令不对拒绝。
		if _, err := fac.Login(from1, "sa", "wrong"); !errors.Is(err, domain.ErrInvalidCredentials) {
			// 前四次就锁了，阈值不对。
			t.Fatalf("fail %d: %v", i, err)
		}
	}
	// 第五次在示教器登录错，两条路共用计数。
	if _, err := fac.LoginPad(from1, "SA ", "wrong"); !errors.Is(err, domain.ErrInvalidCredentials) {
		// 第五次本身仍按口令不对回。
		t.Fatalf("fail 5: %v", err)
	}
	// 锁住后口令对也要拒绝。
	if _, err := fac.Login(from1, "sa", "sa-pass"); !errors.Is(err, domain.ErrTooManyAttempts) {
		// 锁没生效。
		t.Fatalf("locked web: %v", err)
	}
	// 示教器登录同样被锁。
	if _, err := fac.LoginPad(from1, "sa", "sa-pass"); !errors.Is(err, domain.ErrTooManyAttempts) {
		// 锁没覆盖示教器登录。
		t.Fatalf("locked pad: %v", err)
	}
	// 换一台来源不受影响。
	if _, err := fac.Login(from2, "sa", "sa-pass"); err != nil {
		// 一个来源错多了把别处也锁了。
		t.Fatalf("other source: %v", err)
	}
	// 不存在的登录名错多了也锁，挡住逐个试名字。
	for i := 0; i < 5; i++ {
		// 没有这个人应按凭证不对拒绝。
		if _, err := fac.Login(from2, "ghost", "x"); !errors.Is(err, domain.ErrInvalidCredentials) {
			// 前五次就锁了或回了别的错。
			t.Fatalf("ghost %d: %v", i, err)
		}
	}
	// 第六次应已锁住。
	if _, err := fac.Login(from2, "ghost", "x"); !errors.Is(err, domain.ErrTooManyAttempts) {
		// 试名字没被锁。
		t.Fatalf("ghost locked: %v", err)
	}
}
