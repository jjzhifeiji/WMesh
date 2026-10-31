// 本机上行回执：审计记会话里的登录人，资产乱填不拼，坏上行不逐条写审计。
package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/id"
	factory "wmesh/factory/internal/service"
)

// 回执审计的操作者和对象按会话认，带正文或解不开的上行不写审计
func TestClientUpAuditActorAndTarget(t *testing.T) {
	// 准备空上下文，后续调用都挂在这上面
	ctx := context.Background()
	// 起一套测试库和厂服务，起不来则本例无法开始
	h := New(t)
	// 建厂并种初始超管，失败则本步验收不能继续
	_, fac, err := h.Provision(ctx, "sa", "超管")
	// 建厂并种初始超管失败就停，避免带着错误继续验
	if err != nil {
		// 建厂并种初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 激活初始超管失败就停，避免带着错误继续验
	if err := seedPass(ctx, fac, "sa", "sa-pass"); err != nil {
		// 激活初始超管失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 登录取出令牌，登不上则后面没有身份
	saTok := mustLogin(t, ctx, fac, "sa", "sa-pass")
	// 建一名操作员，回执就由他的会话送上来
	op := mustCreateRole(t, ctx, fac, saTok, "op", "op-pass", factory.RoleOperator)
	// 会话里鉴权过的那台本机
	cid := id.New()

	// 资产号超长又不是身份，只能记本机
	raw, _ := json.Marshal(map[string]any{"typ": "ack", "assetId": strings.Repeat("a", 4096), "revision": 1})
	// 记下送上行前已有的回执审计
	before := clientAckRows(t, ctx, fac)
	// 从会话那台送上回执
	fac.HandleClientUp(ctx, op.acc.ID, cid, raw)
	// 取出这次新增的那一条回执审计
	row := oneNewAck(t, before, clientAckRows(t, ctx, fac))
	// 对象混进了乱填的资产号，或操作者不是登录人即失败
	if row.Target != cid.String() || row.ActorID == nil || *row.ActorID != op.acc.ID || row.Result != audit.Allow {
		// 打出这条审计，便于看错在哪
		t.Fatalf("long asset ack %+v", row)
	}

	// 资产号是正经身份，审计对象要带上它
	asset := id.New()
	// 资产号用大写写法，审计里应收成规范写法
	raw, _ = json.Marshal(map[string]any{"typ": "ack", "assetId": strings.ToUpper(asset.String()), "revision": 2})
	// 记下送上行前已有的回执审计
	before = clientAckRows(t, ctx, fac)
	// 从会话那台送上回执
	fac.HandleClientUp(ctx, op.acc.ID, cid, raw)
	// 取出这次新增的那一条回执审计
	row = oneNewAck(t, before, clientAckRows(t, ctx, fac))
	// 对象没带上规范资产号，或操作者不对即失败
	if row.Target != cid.String()+" "+asset.String() || row.ActorID == nil || *row.ActorID != op.acc.ID {
		// 打出这条审计，便于看错在哪
		t.Fatalf("uuid asset ack %+v", row)
	}

	// 会话没认出人时操作者留空，不冒充别人
	before = clientAckRows(t, ctx, fac)
	// 用空登录人送上同一条回执
	fac.HandleClientUp(ctx, uuid.Nil, cid, raw)
	// 取出这次新增的那一条回执审计
	row = oneNewAck(t, before, clientAckRows(t, ctx, fac))
	// 空登录人却记了操作者即失败
	if row.ActorID != nil {
		// 打出这条审计，便于看错在哪
		t.Fatalf("nil person ack %+v", row)
	}

	// 带正文、不是 JSON、字段类型不对的上行各送一条
	bad := [][]byte{
		[]byte(`{"typ":"ack","assetId":"` + asset.String() + `","content":"WM2AAAA"}`),
		[]byte("not json"),
		[]byte(`{"typ":"ack","assetId":123}`),
	}
	// 记下送坏上行前已有的回执审计
	before = clientAckRows(t, ctx, fac)
	// 逐条送上坏上行，每条都不该写审计
	for _, b := range bad {
		// 从会话那台送上这条坏上行
		fac.HandleClientUp(ctx, op.acc.ID, cid, b)
	}
	// 坏上行写了任何回执审计即失败
	if after := clientAckRows(t, ctx, fac); len(after) != len(before) {
		// 打出前后条数，便于看多写了几条
		t.Fatalf("bad ups wrote audit: %d -> %d", len(before), len(after))
	}
}

// 读出全部回执审计，按条目身份收成表
func clientAckRows(t *testing.T, ctx context.Context, fac *factory.Service) map[uuid.UUID]audit.Row {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 读出本厂审计，读不到就没法比
	rows, err := fac.ListAudit(ctx)
	// 读审计失败就停，避免带着错误继续验
	if err != nil {
		// 读审计失败就把原因打出并停掉本例
		t.Fatal(err)
	}
	// 准备按身份收下回执审计
	out := map[uuid.UUID]audit.Row{}
	// 逐行挑出回执动作的审计
	for _, r := range rows {
		// 只收回执动作，允许和拒绝都算
		if r.Action == "client_ack" {
			// 收下这一行，后面按身份比新增
			out[r.ID] = r
		}
	}
	return out
}

// 前后两份回执审计相比只多出一条，交回这一条
func oneNewAck(t *testing.T, before, after map[uuid.UUID]audit.Row) audit.Row {
	// 标成辅助函数，失败栈落到调用方
	t.Helper()
	// 准备收下新增的行
	var fresh []audit.Row
	// 逐行看是不是这次新增的
	for k, r := range after {
		// 之前没有的才算新增
		if _, ok := before[k]; !ok {
			// 收下这条新增的审计
			fresh = append(fresh, r)
		}
	}
	// 新增不是正好一条即失败
	if len(fresh) != 1 {
		// 打出新增的全部行，便于看多了还是少了
		t.Fatalf("new client_ack rows = %+v, want 1", fresh)
	}
	return fresh[0]
}
