// F1：本厂一行 Client 策略；超管可改，非超管拒绝，修订只向前。
package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	factory "wmesh/factory/internal/service"
)

// 超管能改客户端策略，非超管要拒绝，修订只向前。
func TestClientPolicy(t *testing.T) {
	// 准备无取消的上下文，后面每次调用都挂在上面。
	ctx := context.Background()
	// 拉起隔离厂库，起不来说明测试库还没就绪。
	h := New(t)
	// 建厂并签发超管激活码，失败则没有厂可测。
	seed, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把建厂的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 开通失败就停，否则后面没有可靠结果。
	if err := fac.Activate(ctx, "sa", seed.ActivationToken, "sa-pass"); err != nil {
		// 把开通的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 登录并取出令牌，失败说明账号没有开通成功。
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建人并授好角色，失败说明后面没有账号可用。
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator, factory.ScopeFactory, nil)

	// 读回客户端策略，失败说明没权读或没设过。
	got, err := fac.GetClientPolicy(ctx, saTok)
	// 读策略失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把读策略的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 修订号应按这次保存前进，没变说明写没落库。
	if got.Revision != 0 || got.MaxCachedProjects != 2 || got.CacheScope != factory.CacheScopeAll ||
		got.PersistUnwrapKey || got.KeyTTLSeconds != 0 || !got.EncryptPouch || !bytes.Equal(got.Extra, []byte(`{}`)) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("default %+v extra=%s", got, got.Extra)
	}

	// 包成原始报文，结构错了签名会对不上。
	wantExtra := json.RawMessage(`{"note":1}`)
	// 改写客户端策略，失败说明越权或字段不合法。
	saved, err := fac.SetClientPolicy(ctx, saTok, factory.ClientPolicy{
		MaxCachedProjects: 4,
		CacheScope:        factory.CacheScopeCurrent,
		PersistUnwrapKey:  true,
		KeyTTLSeconds:     3600,
		EncryptPouch:      true,
		Extra:             wantExtra,
	})
	// 改策略失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把改策略的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 修订号应按这次保存前进，没变说明写没落库。
	if saved.Revision != 1 || saved.MaxCachedProjects != 4 || saved.CacheScope != factory.CacheScopeCurrent ||
		!saved.PersistUnwrapKey || saved.KeyTTLSeconds != 3600 || !saved.EncryptPouch {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("saved %+v", saved)
	}
	// 留出额外字段，策略里多出来说明没按白名单收。
	var extra map[string]any
	// 解额外字段失败或结果不符就停，说明没达预期。
	if err := json.Unmarshal(saved.Extra, &extra); err != nil || extra["note"] != float64(1) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("extra %s", saved.Extra)
	}

	// 改策略应因越权被拒绝，放行说明没拦住。
	if _, err := fac.SetClientPolicy(ctx, saTok, factory.ClientPolicy{
		// 包成原始报文，结构错了签名会对不上。
		MaxCachedProjects: 4, CacheScope: "both", Extra: json.RawMessage(`{}`),
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("bad scope: %v", err)
	}
	// 改策略应因越权被拒绝，放行说明没拦住。
	if _, err := fac.SetClientPolicy(ctx, saTok, factory.ClientPolicy{
		// 包成原始报文，结构错了签名会对不上。
		MaxCachedProjects: 4, CacheScope: factory.CacheScopeCurrent, KeyTTLSeconds: -1, Extra: json.RawMessage(`{}`),
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("bad ttl: %v", err)
	}
	// 改策略应因越权被拒绝，放行说明没拦住。
	if _, err := fac.SetClientPolicy(ctx, saTok, factory.ClientPolicy{
		// 包成原始报文，结构错了签名会对不上。
		MaxCachedProjects: 0, CacheScope: factory.CacheScopeCurrent, Extra: json.RawMessage(`{}`),
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("bad max: %v", err)
	}
	// 读回客户端策略，失败说明没权读或没设过。
	still, err := fac.GetClientPolicy(ctx, saTok)
	// 读策略失败或修订不对就停，说明没达预期。
	if err != nil || still.Revision != 1 || still.MaxCachedProjects != 4 {
		// 非法输入被收下了，说明校验没有生效。
		t.Fatalf("unchanged after invalid %+v %v", still, err)
	}

	// 读策略应因越权被拒绝，放行说明没拦住。
	if _, err := fac.GetClientPolicy(ctx, op.tok); !errors.Is(err, domain.ErrForbidden) {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("op get: %v", err)
	}
	// 改策略应因越权被拒绝，放行说明没拦住。
	if _, err := fac.SetClientPolicy(ctx, op.tok, factory.ClientPolicy{
		// 包成原始报文，结构错了签名会对不上。
		MaxCachedProjects: 9, CacheScope: factory.CacheScopeAll, Extra: json.RawMessage(`{}`),
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("op set: %v", err)
	}
	// 读回客户端策略，失败说明没权读或没设过。
	afterDeny, err := fac.GetClientPolicy(ctx, saTok)
	// 读策略失败或修订不对就停，说明没达预期。
	if err != nil || afterDeny.Revision != 1 || afterDeny.MaxCachedProjects != 4 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("op must not write %+v %v", afterDeny, err)
	}

	// 改缓存上限失败就停，否则后面没有可靠结果。
	if err := fac.SetCacheLimit(ctx, saTok, 5); err != nil {
		// 把改缓存上限的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 读回客户端策略，失败说明没权读或没设过。
	afterLimit, err := fac.GetClientPolicy(ctx, saTok)
	// 读策略失败或修订不对就停，说明没达预期。
	if err != nil || afterLimit.Revision != 2 || afterLimit.MaxCachedProjects != 5 ||
		afterLimit.CacheScope != factory.CacheScopeCurrent || !afterLimit.PersistUnwrapKey || !afterLimit.EncryptPouch {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("cache limit %+v %v", afterLimit, err)
	}

	// 改写客户端策略，失败说明越权或字段不合法。
	plain, err := fac.SetClientPolicy(ctx, saTok, factory.ClientPolicy{
		MaxCachedProjects: 5,
		CacheScope:        factory.CacheScopeCurrent,
		PersistUnwrapKey:  true,
		KeyTTLSeconds:     3600,
		EncryptPouch:      false,
		Extra:             wantExtra,
	})
	// 改策略失败或修订不对就停，说明没达预期。
	if err != nil || plain.EncryptPouch || plain.Revision != 3 {
		// 断言没通过就停，结果和这条用例的预期不一致。
		t.Fatalf("encrypt off %+v %v", plain, err)
	}

	// 拉出审计流水，失败则无法核对有没有记账。
	rows, err := fac.ListAudit(ctx)
	// 拉审计失败就停，否则后面没有可靠结果。
	if err != nil {
		// 把拉审计的错误打出来并停住，避免继续往下验。
		t.Fatal(err)
	}
	// 审计应留下允许或拒绝，缺了说明这步没记账。
	if bad := audit.Incomplete(rows); len(bad) > 0 {
		// 审计里缺了这条记录，说明这一步没有记账。
		t.Fatalf("incomplete audit %#v", bad[0])
	}
	// 把审计打成可搜文本，打不出则查不了泄密。
	dump := audit.Dump(rows)
	// 审计里不应出现口令或正文，出现了就算泄密。
	if !audit.ContainsAny(dump, "set_client_policy") {
		// 应存在的记录没有出现，说明这一步没落下。
		t.Fatalf("missing set_client_policy: %s", dump)
	}
	// 激活码应是八位，位数不对说明签发坏了。
	if audit.ContainsAny(dump, "sa-pass", "op-pass", seed.ActivationToken, saTok) {
		// 结果里出现了不该有的敏感词，说明已经泄密。
		t.Fatalf("secret leaked")
	}
}
