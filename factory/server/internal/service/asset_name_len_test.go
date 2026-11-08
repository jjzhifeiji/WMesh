// 工艺、工程名不超过焊次能收的工程名上限，免得建得出的工程焊完后焊次永远收不下（D-79）。
package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 网页新建、改名、示教器新建都按 200 字封顶，超了明说名字太长；恰好 200 字照常。
func TestAssetNameLengthMatchesWeldLimit(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并写下初始超管，失败则没有厂可测。
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住。
		t.Fatal(err)
	}
	// 超管用默认口令登录拿会话。
	tok := mustLogin(t, ctx, fac, "sa", personPass("sa"))
	// 恰好上限的名字，多字节字按字数算。
	okName := strings.Repeat("长", store.WeldMaxProjectName)
	// 多一个字的名字。
	longName := okName + "长"
	// 超长名字建厂级工艺应拒，原因明说太长。
	if _, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{}, longName, []byte(`{}`)); !errors.Is(err, domain.ErrNameTooLong) {
		// 放行就停住。
		t.Fatalf("create long: %v", err)
	}
	// 恰好上限照常建。
	row, err := fac.CreateFactoryProcess(ctx, tok, store.WorkContext{}, okName, []byte(`{}`))
	// 建不出就停住。
	if err != nil {
		// 把错误打出来并停住。
		t.Fatalf("create at limit: %v", err)
	}
	// 改成超长名字应拒。
	if _, err := fac.RenameAsset(ctx, tok, row.ID, row.Revision, longName); !errors.Is(err, domain.ErrNameTooLong) {
		// 放行就停住。
		t.Fatalf("rename long: %v", err)
	}
	// 示教器新建超长名字同样拒。
	if _, err := fac.CreatePad(ctx, tok, "", factory.KindProcess, longName, []byte(`{}`), uuid.Nil, "", nil); !errors.Is(err, domain.ErrNameTooLong) {
		// 放行就停住。
		t.Fatalf("pad create long: %v", err)
	}
	// 另存成超长名字同样拒。
	if _, err := fac.CopyProcess(ctx, tok, row.ID, longName); !errors.Is(err, domain.ErrNameTooLong) {
		// 放行就停住。
		t.Fatalf("copy long: %v", err)
	}
}
