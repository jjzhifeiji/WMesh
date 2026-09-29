// C1：本机袋未登录解不开；默认钥不落盘；退出换人清库。
package service_test

import (
	"bytes"
	"testing"

	"wmesh/factory/internal/platform/contentcrypt"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
	"wmesh/factory/internal/store"
)

// 本机袋未登录解不开，能解开说明门没关上。
func TestPouchRequiresLogin(t *testing.T) {
	// 建立一只空的本地工艺包，失败则无法装包。
	p := factory.NewPouch()
	// 新取一个编号，撞号会让两条记录分不清。
	aid := id.New()
	// 新取一个编号，撞号会让两条记录分不清。
	who := id.New()
	// 写明文应因未登录被拒绝，放行说明没拦住。
	if err := p.PutPlain(aid, store.AssetLevelFactory, "厂级", 1, nil, []byte(`{"n":1}`)); err != domain.ErrUnauthorized {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("put: %v", err)
	}
	// 开袋内稿应因未登录被拒绝，放行说明没拦住。
	if _, err := p.Open(aid); err != domain.ErrUnauthorized {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("open: %v", err)
	}
	// 生成正文密钥，失败则工艺包无法加密。
	key, err := contentcrypt.RandomKey()
	// 生成密钥失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把生成密钥的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录本机袋失败就停，否则后面没有可靠结果。
	if err := p.Login(key, who, false); err != nil {
		// 把登录本机袋的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 写明文失败就停，否则后面没有可靠结果。
	if err := p.PutPlain(aid, store.AssetLevelFactory, "厂级", 1, nil, []byte(`{"n":1}`)); err != nil {
		// 把写明文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 从本机袋打开这份稿，失败说明没登录或没有这份。
	got, err := p.Open(aid)
	// 开袋内稿失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(got, []byte(`{"n":1}`)) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("open %s %v", got, err)
	}
	// 退出本机袋，之后钥匙和明文都不应还在。
	p.Logout()
	// 退出后不应还留着解开钥匙，还在说明没清干净。
	if p.HasUnwrapKey() {
		// 解开钥匙还在内存里，说明退出没有清掉。
		t.Fatal("key still in memory")
	}
	// 开袋内稿应因未登录被拒绝，放行说明没拦住。
	if _, err := p.Open(aid); err != domain.ErrUnauthorized {
		// 退出之后仍能打开，说明换人没有清库。
		t.Fatalf("after logout: %v", err)
	}
	// 按材料重锁应因未登录被拒绝，放行说明没拦住。
	if err := p.RelockFromMaterial(); err != domain.ErrUnauthorized {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("persist false relock: %v", err)
	}
	// 逐条看落盘内容，出现钥匙或明文说明没清干净。
	for _, env := range p.DiskSnapshot() {
		// 正文里不应残留这段，还在说明旧结构没清掉。
		if bytes.Contains(env.Blob, key) || bytes.Contains(env.Blob, []byte(`{"n":1}`)) {
			// 结果里出现了不该有的敏感词，说明已经泄密。
			t.Fatal("disk leaked key or plaintext")
		}
	}
}

// 退出换人后应清掉上一人的库，还在说明没换干净。
func TestPouchPersonalFollowsOwner(t *testing.T) {
	// 建立一只空的本地工艺包，失败则无法装包。
	p := factory.NewPouch()
	// 生成正文密钥，失败则工艺包无法加密。
	key, err := contentcrypt.RandomKey()
	// 生成密钥失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把生成密钥的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新取一个编号，撞号会让两条记录分不清。
	a := id.New()
	// 新取一个编号，撞号会让两条记录分不清。
	b := id.New()
	// 新取一个编号，撞号会让两条记录分不清。
	pid := id.New()
	// 登录本机袋失败就停，否则后面没有可靠结果。
	if err := p.Login(key, a, false); err != nil {
		// 把登录本机袋的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 写明文失败就停，否则后面没有可靠结果。
	if err := p.PutPlain(pid, store.AssetLevelPersonal, "我的", 1, &a, []byte(`{"p":1}`)); err != nil {
		// 把写明文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新取一个编号，撞号会让两条记录分不清。
	fid := id.New()
	// 写明文失败就停，否则后面没有可靠结果。
	if err := p.PutPlain(fid, store.AssetLevelFactory, "厂级", 1, nil, []byte(`{"f":1}`)); err != nil {
		// 把写明文的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 退出本机袋，之后钥匙和明文都不应还在。
	p.Logout()
	// 登录本机袋失败就停，否则后面没有可靠结果。
	if err := p.Login(key, b, false); err != nil {
		// 把登录本机袋的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开袋内稿应因找不到被拒绝，放行说明没拦住。
	if _, err := p.Open(pid); err != domain.ErrNotFound {
		// 个人稿串到了别人名下，说明归属没保住。
		t.Fatalf("personal: %v", err)
	}
	// 开袋内稿应因找不到被拒绝，放行说明没拦住。
	if _, err := p.Open(fid); err != domain.ErrNotFound {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("factory after wipe: %v", err)
	}
}

// 只有更新的明文能覆盖，旧稿盖住说明比较反了。
func TestPouchPutPlainIfNewer(t *testing.T) {
	// 建立一只空的本地工艺包，失败则无法装包。
	p := factory.NewPouch()
	// 生成正文密钥，失败则工艺包无法加密。
	key, err := contentcrypt.RandomKey()
	// 生成密钥失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把生成密钥的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新取一个编号，撞号会让两条记录分不清。
	who := id.New()
	// 登录本机袋失败就停，否则后面没有可靠结果。
	if err := p.Login(key, who, false); err != nil {
		// 把登录本机袋的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 新取一个编号，撞号会让两条记录分不清。
	aid := id.New()
	// 较新才写入明文，失败说明修订比较被拒。
	ok, err := p.PutPlainIfNewer(aid, store.AssetLevelFactory, "厂级", 2, nil, []byte(`{"n":2}`))
	// 写较新明文失败或结果不符就停，说明没达预期。
	if err != nil || !ok {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("put2 %v %v", ok, err)
	}
	// 较新才写入明文，失败说明修订比较被拒。
	ok, err = p.PutPlainIfNewer(aid, store.AssetLevelFactory, "厂级", 1, nil, []byte(`{"n":1}`))
	// 写较新明文失败或结果不符就停，说明没达预期。
	if err != nil || ok {
		// 旧修订盖住了新稿，说明先后比较写反了。
		t.Fatalf("stale %v %v", ok, err)
	}
	// 从本机袋打开这份稿，失败说明没登录或没有这份。
	got, err := p.Open(aid)
	// 开袋内稿失败或正文不同就停，说明没达预期。
	if err != nil || !bytes.Equal(got, []byte(`{"n":2}`)) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("held %s %v", got, err)
	}
}
