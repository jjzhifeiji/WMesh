// 工程正文里的包角几何：给人读的入口遮掉，网页保存盖回，示教器闭包是全的。
package service_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// cornerBody 拼一份带包角组的单层工程：一条普通焊道和一条包角焊道，层参数用好认的数。
func cornerBody(proc uuid.UUID) []byte {
	// 普通焊道的点列要留着，包角焊道的点是生成的。
	return []byte(`[{"id":"w1","name":"普通","templateId":"11111111-1111-4111-8111-111111111111","processId":"` + proc.String() + `","points":[{"x":11.5}]},` +
		`{"id":"w2","name":"包角","templateId":"11111111-1111-4111-8111-111111111111","processId":"` + proc.String() + `","points":[{"x":987.25}],` +
		`"cornerGroupParams":{"groupId":"g1","refPathAId":"w1","refPathBId":"w1","layerCount":3,"initialLength":123.5,"upwardOffset":45.5,"lengthReduction":6.5,"isMaster":true,"processId":"` + proc.String() + `","passes":[{"length":777.5,"flatOffset":1.5,"upwardOffset":2.5}]}}]`)
}

// 读正文遮掉包角几何，谁读都一样；网页保存盖回；示教器闭包是全的；快照入口不交。
func TestCornerGeometryMasked(t *testing.T) {
	// 开厂建人。
	e := newAdoptEnv(t)
	// 建一条厂级工艺给工程引用。
	proc, err := e.fac.CreateFactoryProcess(e.ctx, e.admin.tok, factory.WorkContext{}, "厂级工艺", []byte(`{"name":"f"}`))
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 发布成可用。
	proc, err = e.fac.PublishAsset(e.ctx, e.admin.tok, proc.ID, proc.Revision)
	// 发布失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 管理员在示教器上存的工程，正文带包角层参数。
	proj, err := e.fac.CreatePad(e.ctx, e.admin.tok, factory.AssetLevelFactory, factory.KindProject, "带包角", cornerBody(proc.ID), id.New(), "", nil, "single")
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 超管、管理员、操作员读到的都没有这些数。
	for _, tok := range []string{e.sa, e.admin.tok, e.owner.tok} {
		// 读正文。
		raw, err := e.fac.ReadAssetContent(e.ctx, tok, proj.ID)
		// 工程不保密，都读得到。
		if err != nil {
			// 把错误打出来。
			t.Fatal(err)
		}
		// 几何和生成的点列都不能出现。
		for _, secret := range []string{"123.5", "45.5", "777.5", "987.25", "layerCount", "passes"} {
			// 出现就是漏了。
			if strings.Contains(string(raw), secret) {
				// 把结果打出来。
				t.Fatalf("leak %s in %s", secret, raw)
			}
		}
		// 普通焊道和包角组身份都还在，页面照常画。
		if !strings.Contains(string(raw), "11.5") || !strings.Contains(string(raw), `"groupId":"g1"`) {
			// 把结果打出来。
			t.Fatalf("over masked %s", raw)
		}
	}
	// 快照入口不交带包角几何的工程。
	if _, err := e.fac.ExportAssetSnapshot(e.ctx, e.admin.tok, proj.ID); !errors.Is(err, domain.ErrForbidden) {
		// 把结果打出来。
		t.Fatalf("snapshot %v", err)
	}
	// 网页拿遮过的正文改个焊道名再存。
	masked, err := e.fac.ReadAssetContent(e.ctx, e.admin.tok, proj.ID)
	// 读失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 只改名字。
	edited := bytes.ReplaceAll(masked, []byte("普通"), []byte("改过"))
	// 工程当前修订。
	cur, err := e.fac.GetAsset(e.ctx, e.admin.tok, proj.ID)
	// 读失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 保存。
	saved, err := e.fac.UpdateAssetContent(e.ctx, e.admin.tok, proj.ID, cur.Revision, edited)
	// 不该拒。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 回包不能把盖回的几何带出去。
	if saved.SavedContent != nil && strings.Contains(*saved.SavedContent, "777.5") {
		// 把结果打出来。
		t.Fatalf("response leak %s", *saved.SavedContent)
	}
	// 示教器闭包里几何还是全的，名字是改过的：操作员登录平板拉。
	sess := padSyncLogin(t, e.ctx, e.fac, "owner", "owner-pass")
	// 拉工程包。
	pulled, err := e.fac.PadPullClientClosure(e.ctx, sess.Token, proj.ID)
	// 拉失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 用登录人钥解开。
	opened, err := factory.OpenTransit(e.fac.Store().FactoryID(), sess.Account.ID, sess.UnwrapKey, pulled)
	// 解不开就停。
	if err != nil || len(opened.Members) != 1 {
		// 把错误打出来。
		t.Fatalf("open %+v %v", opened, err)
	}
	// 焊接要用的几何和点列都在。
	body := string(opened.Members[0].Content)
	// 缺一样就是把几何弄丢了。
	for _, want := range []string{"123.5", "45.5", "777.5", "987.25", "改过"} {
		// 没有就是丢了。
		if !strings.Contains(body, want) {
			// 把结果打出来。
			t.Fatalf("lost %s in %s", want, body)
		}
	}
	// 示教器盖正文照收全的，不被盖回。
	next := bytes.ReplaceAll(cornerBody(proc.ID), []byte("777.5"), []byte("555.5"))
	// 管理员示教器会话。
	admin := padSyncLogin(t, e.ctx, e.fac, "admin", "admin-pass")
	// 盖正文。
	if _, err := e.fac.ApplyAppContent(e.ctx, admin.Token, proj.ID, next); err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 再拉一次。
	pulled, err = e.fac.PadPullClientClosure(e.ctx, admin.Token, proj.ID)
	// 拉失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 解开。
	opened, err = factory.OpenTransit(e.fac.Store().FactoryID(), admin.Account.ID, admin.UnwrapKey, pulled)
	// 新几何生效。
	if err != nil || !strings.Contains(string(opened.Members[0].Content), "555.5") {
		// 把结果打出来。
		t.Fatalf("pad body %+v %v", opened, err)
	}
}

// 没有包角几何的工程读回字节不动。
func TestCornerMaskLeavesPlainProject(t *testing.T) {
	// 开厂建人。
	e := newAdoptEnv(t)
	// 正文写法和排版都要原样留着。
	body := []byte(`[ {"name":"普通 <焊道>"} ]`)
	// 建工程。
	proj, err := e.fac.Store().InsertGovernedAssetInto(e.ctx, factory.Asset{
		Kind: factory.KindProject, Level: factory.AssetLevelFactory, Name: "普通", Status: factory.AssetAvailable, Copyable: true, WeldKind: "single",
		Content: body, Digest: digest.Sum(body), CreatorID: e.admin.acc.ID,
	}, uuid.Nil, false)
	// 建失败就停。
	if err != nil {
		// 把错误打出来。
		t.Fatal(err)
	}
	// 读回。
	got, err := e.fac.ReadAssetContent(e.ctx, e.admin.tok, proj.ID)
	// 字节不动。
	if err != nil || !bytes.Equal(got, body) {
		// 把结果打出来。
		t.Fatalf("plain %s %v", got, err)
	}
}
