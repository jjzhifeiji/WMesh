// 测试缺陷回归：新密码长度、设备识别号大小写判重、重置一次性、工程模版删除后补发全集。
package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	global "wmesh/global/internal/service"
)

// 改密、引导、重置都要求新密码至少 8 个字，绕过前端也设不进弱口令（D-02）。
func TestPasswordMinLength(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 引导口令太短应被拒，库里不落管理员。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "short"); !errors.Is(err, domain.ErrPasswordTooShort) {
		// 不符即停：短口令引导没被拦住。
		t.Fatalf("bootstrap short: %v", err)
	}
	// 立超管并登录，后面都用这张票。
	tok := retiredClientLogin(t, h)
	// 改成 5 个字的新密码应被拒。
	if err := h.WAN.ChangePassword(ctx, tok, "wan-secret", "short"); !errors.Is(err, domain.ErrPasswordTooShort) {
		// 不符即停：短新密码没被拦住。
		t.Fatalf("change short: %v", err)
	}
	// 被拒后旧密码照样能登录。
	if _, err := h.WAN.Login(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：被拒的改密却改掉了旧密码。
		t.Fatalf("old pass after reject: %v", err)
	}
	// 按字数算：8 个汉字够长，应能改成。
	if err := h.WAN.ChangePassword(ctx, tok, "wan-secret", "八个汉字的新密码"); err != nil {
		// 不符即停：够长的新密码被误拒。
		t.Fatalf("change 8 runes: %v", err)
	}
	// 重置口令太短同样被拒。
	if err := h.WAN.ResetAdminPassword(ctx, "w", "1234567"); !errors.Is(err, domain.ErrPasswordTooShort) {
		// 不符即停：短口令重置没被拦住。
		t.Fatalf("reset short: %v", err)
	}
}

// 识别号按大写存、大小写不论判重；超长报超长而不是必填（D-03）。
func TestDeviceSerialCaseInsensitive(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立超管并登录，后面都用这张票。
	tok := retiredClientLogin(t, h)
	// 先登记小写号，应收成大写存。
	c, err := h.WAN.RegisterClient(ctx, tok, "焊机甲", id.New(), uuid.Nil, nil, " fa:ke:00:00:00:01 ")
	// 登记失败或没收成大写就停。
	if err != nil || c.DeviceSerial != "FA:KE:00:00:00:01" {
		// 不符即停：识别号没收成大写。
		t.Fatalf("register lower: %+v %v", c, err)
	}
	// 大写同号再登记应被拒为已登记。
	if _, err := h.WAN.RegisterClient(ctx, tok, "焊机乙", id.New(), uuid.Nil, nil, "FA:KE:00:00:00:01"); !errors.Is(err, domain.ErrDeviceSerialTaken) {
		// 不符即停：大小写不同的同号又登记了一台。
		t.Fatalf("register upper dup: %v", err)
	}
	// 混写的同号同样被拒。
	if _, err := h.WAN.RegisterClient(ctx, tok, "焊机丙", id.New(), uuid.Nil, nil, "Fa:Ke:00:00:00:01"); !errors.Is(err, domain.ErrDeviceSerialTaken) {
		// 不符即停：混写同号又登记了一台。
		t.Fatalf("register mixed dup: %v", err)
	}
	// 129 字应报超长。
	if _, err := h.WAN.RegisterClient(ctx, tok, "焊机丁", id.New(), uuid.Nil, nil, strings.Repeat("a", 129)); !errors.Is(err, domain.ErrDeviceSerialTooLong) {
		// 不符即停：超长报成了别的错。
		t.Fatalf("too long: %v", err)
	}
	// 空号仍报必填。
	if _, err := h.WAN.RegisterClient(ctx, tok, "焊机戊", id.New(), uuid.Nil, nil, "  "); !errors.Is(err, domain.ErrDeviceSerialRequired) {
		// 不符即停：空号没报必填。
		t.Fatalf("empty: %v", err)
	}
}

// 按配置重置只生效一次：同一口令再启动不盖回，换新口令才再生效（D-33）。
func TestAdminResetOnce(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立超管并登录，后面都用这张票。
	tok := retiredClientLogin(t, h)
	// 第一次按配置口令重置应真的重置。
	applied, err := h.WAN.ResetAdminPasswordOnce(ctx, "w", "env-pass-1")
	// 没重置或出错就停。
	if err != nil || !applied {
		// 不符即停：第一次重置没生效。
		t.Fatalf("first reset %v %v", applied, err)
	}
	// 重置后旧会话作废。
	if _, err := h.WAN.RequireAdmin(ctx, tok); !errors.Is(err, domain.ErrUnauthorized) {
		// 不符即停：重置没作废旧会话。
		t.Fatalf("session after reset: %v", err)
	}
	// 用配置口令登录，接着改成自己的密码。
	tok2, err := h.WAN.Login(ctx, "w", "env-pass-1")
	// 登录失败就停。
	if err != nil {
		// 不符即停：配置口令登录不上。
		t.Fatal(err)
	}
	// 管理员改成自己的密码。
	if err := h.WAN.ChangePassword(ctx, tok2, "env-pass-1", "my-own-pass"); err != nil {
		// 不符即停：改密失败。
		t.Fatal(err)
	}
	// 变量没删、口令没换再启动，应跳过不盖回。
	applied, err = h.WAN.ResetAdminPasswordOnce(ctx, "w", "env-pass-1")
	// 又重置了或出错就停。
	if err != nil || applied {
		// 不符即停：同一口令又重置了一次。
		t.Fatalf("second reset %v %v", applied, err)
	}
	// 管理员自己的密码仍能登录。
	if _, err := h.WAN.Login(ctx, "w", "my-own-pass"); err != nil {
		// 不符即停：自己改的密码被盖掉。
		t.Fatalf("own pass after skip: %v", err)
	}
	// 换了新配置口令才再生效。
	applied, err = h.WAN.ResetAdminPasswordOnce(ctx, "w", "env-pass-2")
	// 没重置或出错就停。
	if err != nil || !applied {
		// 不符即停：换口令后重置没生效。
		t.Fatalf("new env pass %v %v", applied, err)
	}
	// 新配置口令能登录。
	if _, err := h.WAN.Login(ctx, "w", "env-pass-2"); err != nil {
		// 不符即停：新口令登录不上。
		t.Fatalf("login env-pass-2: %v", err)
	}
	// 引导时记过标记的口令，之后同一口令也不再重置。
	if err := h.WAN.MarkAdminReset(ctx, "w", "env-pass-3"); err != nil {
		// 不符即停：记标记失败。
		t.Fatal(err)
	}
	// 记过标记的口令应跳过。
	if applied, err := h.WAN.ResetAdminPasswordOnce(ctx, "w", "env-pass-3"); err != nil || applied {
		// 不符即停：已记标记的口令又重置了。
		t.Fatalf("marked pass %v %v", applied, err)
	}
}

// 删工程模版后推一条全集；删光了也推空集，索引里也带全集（D-04）。
func TestTemplateSetAfterDelete(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起服务并登录，拿到夹具和会话票。
	h, tok := projectTplAdmin(t, ctx)
	// 登记一家有效厂，推送只发给有效厂。
	f, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记失败就停。
	if err != nil {
		// 不符即停：登记工厂失败。
		t.Fatal(err)
	}
	// 换上记录总线，核对推了什么。
	bus := &syncBus{}
	// 换上记录总线。
	h.WAN.SetBus(bus)
	// 列出空库首次落下的种子。
	list, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 列失败或没有种子就停。
	if err != nil || len(list) < 2 {
		// 不符即停：种子不齐。
		t.Fatalf("seed %d %v", len(list), err)
	}
	// 逐份删到零份，每删一份都核对推出的全集。
	for i, row := range list {
		// 删一份。
		if err := h.WAN.DeleteProjectTemplate(ctx, tok, row.ID); err != nil {
			// 不符即停：删工程模版失败。
			t.Fatal(err)
		}
		// 准备接住工程这一类的全集；删模版时工艺全集也会一起推，这里不看。
		var sets []busSent
		// 逐条筛出工程全集。
		for _, sent := range bus.byType(global.CmdTemplateSet) {
			// 种类是工程才收下。
			if sent.cmd.Kind == global.KindProject {
				// 收下这一条。
				sets = append(sets, sent)
			}
		}
		// 每删一份都应有一条全集。
		if len(sets) != i+1 {
			// 不符即停：删除后没推全集。
			t.Fatalf("delete %d: sets=%d", i, len(sets))
		}
		// 最近一条就是这次删除后的全集。
		last := sets[len(sets)-1]
		// 应投给这家厂、标全集、种类是工程，剩下的份数对得上。
		if last.factory != f.Factory.ID || !last.cmd.Full || last.cmd.Kind != global.KindProject || len(last.cmd.TemplateIDs) != len(list)-i-1 {
			// 不符即停：全集内容不对。
			t.Fatalf("delete %d: set %+v", i, last.cmd)
		}
		// 删掉的那份不能还在全集里。
		for _, got := range last.cmd.TemplateIDs {
			// 删掉的身份出现在全集里就不对。
			if got == row.ID.String() {
				// 不符即停：已删的模版仍在全集里。
				t.Fatalf("deleted %s still in set", got)
			}
		}
	}
	// 删光后的索引也要带一条空全集。
	cmds, err := h.WAN.IndexForFactory(ctx, f.Factory.ID, global.KindProject)
	// 组不出索引就停。
	if err != nil {
		// 不符即停：组索引失败。
		t.Fatal(err)
	}
	// 准备数一数全集条数。
	found := 0
	// 逐条找全集。
	for _, c := range cmds {
		// 不是全集就跳过。
		if c.Typ != global.CmdTemplateSet {
			continue
		}
		// 数下这一条。
		found++
		// 删光了应是空集且标全集。
		if !c.Full || len(c.TemplateIDs) != 0 || c.Kind != global.KindProject {
			// 不符即停：空全集内容不对。
			t.Fatalf("index set %+v", c)
		}
	}
	// 只该有一条全集。
	if found != 1 {
		// 不符即停：索引里全集条数不对。
		t.Fatalf("index sets %d", found)
	}
}

// 按原身份补建删掉的工程模版：修订接在删除前之后，并补发含它的全集，厂端才会恢复（D-43）。
func TestTemplateRecreateBumpsRevisionAndSendsSet(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起服务并登录，拿到夹具和会话票。
	h, tok := projectTplAdmin(t, ctx)
	// 登记一家有效厂，推送只发给有效厂。
	f, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记失败就停。
	if err != nil {
		// 不符即停：登记工厂失败。
		t.Fatal(err)
	}
	// 列出空库首次落下的种子。
	list, err := h.WAN.ListProjectTemplates(ctx, tok)
	// 列失败或没有种子就停。
	if err != nil || len(list) == 0 {
		// 不符即停：种子不齐。
		t.Fatalf("seed %d %v", len(list), err)
	}
	// 取第一份种子来删再补建。
	old := list[0]
	// 删一份。
	if err := h.WAN.DeleteProjectTemplate(ctx, tok, old.ID); err != nil {
		// 不符即停：删工程模版失败。
		t.Fatal(err)
	}
	// 换上记录总线，只看补建推了什么。
	bus := &syncBus{}
	// 换上记录总线。
	h.WAN.SetBus(bus)
	// 按原身份、原字段表补建。
	row, err := h.WAN.CreateProjectTemplate(ctx, tok, old.ID, old.Name, old.Schema)
	// 补建失败就停。
	if err != nil {
		// 不符即停：补建失败。
		t.Fatal(err)
	}
	// 修订要比删除前高，厂端才不会当已收过跳过。
	if row.Revision <= old.Revision {
		// 不符即停：补建修订没往上走。
		t.Fatalf("recreate revision %d, old %d", row.Revision, old.Revision)
	}
	// 应推一条带新修订的模版指令。
	tpls := bus.byType(global.CmdTemplate)
	// 没有或修订不对都不行。
	if len(tpls) != 1 || tpls[0].cmd.TemplateID != old.ID.String() || tpls[0].cmd.Revision != row.Revision {
		// 不符即停：模版指令不对。
		t.Fatalf("template cmds %+v", tpls)
	}
	// 还应推一条工程模版全集。
	sets := bus.byType(global.CmdTemplateSet)
	// 应正好一条，投给这家厂、标全集、种类是工程，份数和删除前一样。
	if len(sets) != 1 || sets[0].factory != f.Factory.ID || !sets[0].cmd.Full || sets[0].cmd.Kind != global.KindProject || len(sets[0].cmd.TemplateIDs) != len(list) {
		// 不符即停：补建后没推全集或全集不对。
		t.Fatalf("sets %+v", sets)
	}
	// 准备找补建的这份在不在全集里。
	found := false
	// 逐个核对全集身份。
	for _, id := range sets[0].cmd.TemplateIDs {
		// 找到补建的身份就记下。
		if id == old.ID.String() {
			// 记下找到。
			found = true
		}
	}
	// 补建的那份必须在全集里，厂端才会清掉撤下标记。
	if !found {
		// 不符即停：补建的模版不在全集里。
		t.Fatalf("recreated %s not in set %v", old.ID, sets[0].cmd.TemplateIDs)
	}
	// 再删一次再补建，修订继续往上走，不回落。
	if err := h.WAN.DeleteProjectTemplate(ctx, tok, old.ID); err != nil {
		// 不符即停：第二次删除失败。
		t.Fatal(err)
	}
	// 第二次按原身份补建。
	again, err := h.WAN.CreateProjectTemplate(ctx, tok, old.ID, old.Name, old.Schema)
	// 补建失败或修订没再升都不对。
	if err != nil || again.Revision <= row.Revision {
		// 不符即停：第二次补建修订回落。
		t.Fatalf("second recreate %d (prev %d) %v", again.Revision, row.Revision, err)
	}
}
