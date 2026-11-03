// 云端另存工程：加密工艺保持原引用，普通工艺复制一份；加密开关可来回切。
package service_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/domain"
	global "wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// copyWeldBody 拼一份单层工程正文，每个工艺一条焊道。
func copyWeldBody(procs ...uuid.UUID) []byte {
	// 先放开头的方括号。
	out := []byte("[")
	// 逐个工艺拼一条焊道。
	for i, p := range procs {
		// 第二条起要先接逗号。
		if i > 0 {
			// 接上分隔逗号。
			out = append(out, ',')
		}
		// 接上这条焊道。
		out = append(out, []byte(`{"templateId":"`+contenttpl.SeedTplSingle+`","kind":"single","name":"焊道","processId":"`+p.String()+`"}`)...)
	}
	// 收尾的方括号。
	return append(out, ']')
}

// copyLogin 立超管并登录，交回会话。
func copyLogin(t *testing.T, ctx context.Context, h *Harness) string {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 立云端超管，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 立超管失败就停。
		t.Fatal(err)
	}
	// 登录拿会话。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录失败就停。
	if err != nil {
		// 把登录的错误打出来并停住。
		t.Fatal(err)
	}
	return tok
}

// copyPublishedProcess 建一份平台工艺并发布，交回发布后的行。
func copyPublishedProcess(t *testing.T, ctx context.Context, h *Harness, tok, name string, body []byte, copyable bool) global.Asset {
	// 标成辅助函数，失败栈落到调用方。
	t.Helper()
	// 按指定可复制新建工艺。
	p, err := h.WAN.CreatePlatformProcessWith(ctx, tok, name, body, copyable, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工艺的错误打出来并停住。
		t.Fatal(err)
	}
	// 发布当前修订，工程才钉得上。
	pub, err := h.WAN.PublishPlatformAsset(ctx, tok, p.ID, p.Revision)
	// 发布失败就停。
	if err != nil {
		// 把发布的错误打出来并停住。
		t.Fatal(err)
	}
	return pub
}

// 另存工程：加密工艺保持原身份和修订，普通工艺复制成新的可用工艺，正文和依赖一致。
func TestCopyPlatformProjectKeepsEncryptedCopiesPlain(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 起云端库和服务。
	h := New(t)
	// 立超管并登录。
	tok := copyLogin(t, ctx, h)
	// 加密工艺。
	enc := copyPublishedProcess(t, ctx, h, tok, "加密工艺", []byte(`{"current":166}`), false)
	// 普通工艺的正文。
	plainBody := []byte(`{"current":120}`)
	// 普通工艺。
	plain := copyPublishedProcess(t, ctx, h, tok, "普通工艺", plainBody, true)
	// 建一份工程同时挂两条工艺。
	src, err := h.WAN.CreatePlatformProject(ctx, tok, "源工程", copyWeldBody(enc.ID, plain.ID), nil, store.WeldKindSingle)
	// 建不成或依赖不是两条就停。
	if err != nil || len(src.Deps) != 2 {
		// 把建工程的结果打出来并停住。
		t.Fatalf("project %+v %v", src.Deps, err)
	}
	// 另存前数一下资产。
	before, err := h.WAN.ListPlatformAssets(ctx, tok, "")
	// 列不出就停。
	if err != nil {
		// 把列表错误打出来并停住。
		t.Fatal(err)
	}

	// 另存工程。
	got, err := h.WAN.CopyPlatformProcess(ctx, tok, src.ID, "工程副本")
	// 另存失败就停。
	if err != nil {
		// 把另存的错误打出来并停住。
		t.Fatalf("copy project: %v", err)
	}
	// 副本是新身份的工程草稿。
	if got.ID == src.ID || got.Kind != global.KindProject || got.Status != global.AssetDraft || got.Name != "工程副本" || len(got.Deps) != 2 {
		// 副本元数据不对就停。
		t.Fatalf("copy row %+v", got)
	}
	// 记下普通工艺的新身份。
	var newPlain global.AssetDep
	// 加密工艺是否保持原引用。
	keptEnc := false
	// 逐条核依赖。
	for _, d := range got.Deps {
		// 加密工艺的那一条，修订和摘要都应不变。
		if d.ID == enc.ID {
			// 修订摘要都对上才算保持原引用。
			keptEnc = d.Revision == enc.Revision && bytes.Equal(d.Digest, enc.Digest)
			continue
		}
		// 另一条是普通工艺的新身份。
		newPlain = d
	}
	// 加密工艺没保持原引用，或普通工艺还是原身份，都不对。
	if !keptEnc || newPlain.ID == uuid.Nil || newPlain.ID == plain.ID {
		// 依赖不对就停。
		t.Fatalf("deps %+v", got.Deps)
	}
	// 读复制出的工艺元数据。
	np, err := h.WAN.GetPlatformAsset(ctx, tok, newPlain.ID)
	// 副本应是可用的可复制工艺，修订摘要和依赖一致。
	if err != nil || np.Kind != global.KindProcess || np.Status != global.AssetAvailable || !np.Copyable ||
		np.Revision != newPlain.Revision || !bytes.Equal(np.Digest, newPlain.Digest) {
		// 复制出的工艺不对就停。
		t.Fatalf("copied process %+v %v", np, err)
	}
	// 读原件正文，新建时已套过模版。
	srcBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, plain.ID)
	// 读不到就停。
	if err != nil {
		// 把读正文的错误打出来并停住。
		t.Fatal(err)
	}
	// 读复制出的工艺正文。
	npBody, err := h.WAN.ReadPlatformAssetContent(ctx, tok, newPlain.ID)
	// 正文应和原件一样。
	if err != nil || !bytes.Equal(npBody, srcBody) {
		// 正文不同就停。
		t.Fatalf("copied process body %q %v", npBody, err)
	}
	// 读工程副本正文。
	body, err := h.WAN.ReadPlatformAssetContent(ctx, tok, got.ID)
	// 读不到就停。
	if err != nil {
		// 把读正文的错误打出来并停住。
		t.Fatal(err)
	}
	// 收集副本正文里的工艺引用。
	refs, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), body)
	// 引用应正好是加密工艺和新工艺。
	if err != nil || len(refs) != 2 || refs[0] != enc.ID.String() || refs[1] != newPlain.ID.String() {
		// 正文引用和依赖对不上。
		t.Fatalf("copy refs %v %v", refs, err)
	}
	// 副本草稿直接发布，依赖应仍成立。
	if _, err := h.WAN.PublishPlatformAsset(ctx, tok, got.ID, got.Revision); err != nil {
		// 发布失败说明依赖落成了草稿或对不上。
		t.Fatalf("publish copy: %v", err)
	}
	// 源工程不动。
	still, err := h.WAN.GetPlatformAsset(ctx, tok, src.ID)
	// 源工程修订或依赖变了就停。
	if err != nil || still.Revision != src.Revision || len(still.Deps) != 2 {
		// 源工程被改了。
		t.Fatalf("source %+v %v", still, err)
	}
	// 另存后再数资产。
	after, err := h.WAN.ListPlatformAssets(ctx, tok, "")
	// 应只多出工程副本和一份工艺。
	if err != nil || len(after) != len(before)+2 {
		// 多建或少建了资产。
		t.Fatalf("assets %d -> %d %v", len(before), len(after), err)
	}
}

// 普通工艺不可用时另存工程整次拒绝，不留半截副本。
func TestCopyPlatformProjectRollsBack(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 起云端库和服务。
	h := New(t)
	// 立超管并登录。
	tok := copyLogin(t, ctx, h)
	// 第一条普通工艺，能复制。
	first := copyPublishedProcess(t, ctx, h, tok, "普通一", []byte(`{"current":1}`), true)
	// 第二条普通工艺，稍后停用。
	second := copyPublishedProcess(t, ctx, h, tok, "普通二", []byte(`{"current":2}`), true)
	// 建工程挂两条普通工艺。
	src, err := h.WAN.CreatePlatformProject(ctx, tok, "源工程", copyWeldBody(first.ID, second.ID), nil, store.WeldKindSingle)
	// 建不成就停。
	if err != nil {
		// 把建工程的错误打出来并停住。
		t.Fatal(err)
	}
	// 停用第二条，另存时复制它会失败。
	if _, err := h.WAN.DisablePlatformAsset(ctx, tok, second.ID, second.Revision); err != nil {
		// 停用失败就停。
		t.Fatal(err)
	}
	// 另存前数一下资产。
	before, err := h.WAN.ListPlatformAssets(ctx, tok, "")
	// 列不出就停。
	if err != nil {
		// 把列表错误打出来并停住。
		t.Fatal(err)
	}
	// 另存应因工艺不可用被拒。
	if _, err := h.WAN.CopyPlatformProcess(ctx, tok, src.ID, "副本"); !errors.Is(err, domain.ErrAssetNotAvailable) {
		// 没拦住或错类就停。
		t.Fatalf("copy with disabled dep: %v", err)
	}
	// 再数资产。
	after, err := h.WAN.ListPlatformAssets(ctx, tok, "")
	// 第一条的副本应已回收，资产数不变。
	if err != nil || len(after) != len(before) {
		// 留下了半截副本。
		t.Fatalf("assets %d -> %d %v", len(before), len(after), err)
	}
}

// 加密开关可来回切：不可复制 → 可复制 → 不可复制。
func TestPlatformCopyableTwoWay(t *testing.T) {
	// 准备本测上下文。
	ctx := context.Background()
	// 起云端库和服务。
	h := New(t)
	// 立超管并登录。
	tok := copyLogin(t, ctx, h)
	// 建一份加密工艺。
	p := copyPublishedProcess(t, ctx, h, tok, "开关工艺", []byte(`{"current":1}`), false)
	// 加密改成普通。
	open, err := h.WAN.SetPlatformCopyable(ctx, tok, p.ID, p.Revision, true)
	// 改不成或仍加密就停。
	if err != nil || !open.Copyable || open.Revision <= p.Revision {
		// 加密改普通失败。
		t.Fatalf("false->true %+v %v", open, err)
	}
	// 普通再改回加密。
	closed, err := h.WAN.SetPlatformCopyable(ctx, tok, open.ID, open.Revision, false)
	// 改不成或仍可复制就停。
	if err != nil || closed.Copyable || closed.Revision <= open.Revision {
		// 普通改回加密失败。
		t.Fatalf("true->false %+v %v", closed, err)
	}
}
