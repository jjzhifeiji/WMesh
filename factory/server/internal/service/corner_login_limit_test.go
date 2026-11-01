// G：包角口令随本厂策略下发；登录连续失败到上限就锁。
package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 包角口令默认沿用原值，超管可改，没带就保留，空和超长拒绝，示教器登录能拿到。
func TestCornerPasswordPolicy(t *testing.T) {
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
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")

	// 读回默认策略，包角口令应沿用原来写死的值。
	got, err := fac.GetClientPolicy(ctx, saTok)
	// 读失败或默认值不对就停。
	if err != nil || got.CornerPassword != "bd888888" {
		// 默认值不对，老现场升级后会对不上口令。
		t.Fatalf("default corner %q %v", got.CornerPassword, err)
	}
	// 超管改成新口令，其余项照旧。
	got.CornerPassword = "new-corner"
	// 写入新口令，失败说明校验或权限不对。
	saved, err := fac.SetClientPolicy(ctx, saTok, got)
	// 写失败或没改上就停。
	if err != nil || saved.CornerPassword != "new-corner" {
		// 新口令没落库，示教器拿不到。
		t.Fatalf("set corner %+v %v", saved, err)
	}
	// 这次不带口令，应保留上次的。
	saved.CornerPassword = ""
	// 只改缓存上限，口令留空。
	saved.MaxCachedProjects = 3
	// 写入后读回结果，口令应还是上次的。
	kept, err := fac.SetClientPolicy(ctx, saTok, saved)
	// 写失败或口令被清掉就停。
	if err != nil || kept.CornerPassword != "new-corner" {
		// 改别的项把口令清掉了。
		t.Fatalf("keep corner %+v %v", kept, err)
	}
	// 全是空白的口令要拒绝。
	kept.CornerPassword = "   "
	// 空白口令应被拒绝，放行说明没校验。
	if _, err := fac.SetClientPolicy(ctx, saTok, kept); !errors.Is(err, domain.ErrForbidden) {
		// 空白口令被收下了。
		t.Fatalf("blank corner: %v", err)
	}
	// 超过上限的口令也要拒绝。
	kept.CornerPassword = strings.Repeat("口", 65)
	// 超长口令应被拒绝，放行说明没校验。
	if _, err := fac.SetClientPolicy(ctx, saTok, kept); !errors.Is(err, domain.ErrForbidden) {
		// 超长口令被收下了。
		t.Fatalf("long corner: %v", err)
	}
	// 示教器登录拿到的策略应带上现行口令。
	sess, err := fac.LoginPad(ctx, "sa", "sa-pass")
	// 登录失败或口令不对就停。
	if err != nil || sess.Policy.CornerPassword != "new-corner" {
		// 示教器拿不到口令，离线核对不了。
		t.Fatalf("pad policy corner %q %v", sess.Policy.CornerPassword, err)
	}
}

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
