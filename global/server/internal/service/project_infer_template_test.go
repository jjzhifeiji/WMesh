// 经接口新建平台工程：没写模版身份的焊道按作业类型补种子身份，不再被悄悄吞掉；认不出的模版身份明确拒绝。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/store"
)

// 单层和 T 排平台工程的焊道不带模版身份也照样落库，读回补上对应种子身份。
func TestProjectWithoutTemplateIDInfersSeed(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录失败就停。
	if err != nil {
		// 不符即停：登录失败。
		t.Fatal(err)
	}
	// 新建一份平台 T 排工艺给间隙带引用。
	proc, err := h.WAN.CreatePlatformProcess(ctx, tok, "打底", []byte(`{"name":"p","current":180}`), store.WeldKindTBar)
	// 建工艺失败就停。
	if err != nil {
		// 不符即停：建工艺失败。
		t.Fatal(err)
	}
	// 发布工艺，工程才能钉住它。
	proc, err = h.WAN.PublishPlatformAsset(ctx, tok, proc.ID, proc.Revision)
	// 发布失败就停。
	if err != nil {
		// 不符即停：发布失败。
		t.Fatal(err)
	}
	// 读回正文并核对条数和模版身份。
	readItems := func(id uuid.UUID, n int, tpl string) []map[string]any {
		// 测试辅助，出错时报调用方的行。
		t.Helper()
		// 读回落库的正文。
		got, err := h.WAN.ReadPlatformAssetContent(ctx, tok, id)
		// 读不回就停。
		if err != nil {
			// 不符即停：读正文失败。
			t.Fatal(err)
		}
		// 准备接读回的焊道。
		var arr []map[string]any
		// 拆不开或条数不对就停。
		if err := json.Unmarshal(got, &arr); err != nil || len(arr) != n {
			// 不符即停：焊道被吞或形状不对。
			t.Fatalf("body %s %v", got, err)
		}
		// 每条都要补上对应的种子身份。
		for _, it := range arr {
			// 身份不对就停。
			if it["templateId"] != tpl {
				// 不符即停：带上正文。
				t.Fatalf("templateId want %s in %s", tpl, got)
			}
		}
		return arr
	}
	// T 排焊道去掉模版身份，只留示教器写的种类。
	tbody := strings.NewReplacer("ROOT", proc.ID.String(), "CAP", proc.ID.String(), `,"templateId":"`+tbarSeedID+`"`, "").Replace(padTBarBody)
	// 确认确实去掉了模版身份，免得测成带身份的老路。
	if strings.Contains(tbody, "templateId") {
		// 不符即停：替换没生效。
		t.Fatalf("templateId still in body %s", tbody)
	}
	// 按 T 排新建平台工程。
	tproj, err := h.WAN.CreatePlatformProject(ctx, tok, "T排无身份", []byte(tbody), nil, store.WeldKindTBar)
	// 建工程失败就停。
	if err != nil {
		// 不符即停：建工程失败。
		t.Fatal(err)
	}
	// 读回 T 排工程正文，焊道不能丢，身份补成 T 排种子。
	tarr := readItems(tproj.ID, 1, contenttpl.SeedTplTBar)
	// 逐个专有字段对照。
	for k, want := range tbarPadKeys {
		// 丢了或改了就停。
		if tarr[0][k] != want {
			// 不符即停：带上键和值。
			t.Fatalf("%s: got %v want %v", k, tarr[0][k], want)
		}
	}
	// 单层两条焊道：一条没写身份，一条写了空串，和示教器单层上传的形状一样。
	sbody := `[{"id":"s1","name":"焊道 1"},{"id":"s2","name":"焊道 2","templateId":""}]`
	// 按单层新建平台工程。
	sproj, err := h.WAN.CreatePlatformProject(ctx, tok, "单层无身份", []byte(sbody), nil, store.WeldKindSingle)
	// 建工程失败就停。
	if err != nil {
		// 不符即停：建工程失败。
		t.Fatal(err)
	}
	// 读回单层工程正文，两条都在，身份补成单层种子。
	sarr := readItems(sproj.ID, 2, contenttpl.SeedTplSingle)
	// 名字要原样留下。
	if sarr[0]["name"] != "焊道 1" || sarr[1]["name"] != "焊道 2" {
		// 不符即停：名字丢了。
		t.Fatalf("names lost %v", sarr)
	}
	// 焊道点了库里没有、也不是种子的模版，必须明确拒绝。
	_, err = h.WAN.CreatePlatformProject(ctx, tok, "未知模版", []byte(`[{"name":"x","templateId":"00000000-0000-4000-8000-000000000009"}]`), nil, store.WeldKindSingle)
	// 没拒或拒错了原因就停。
	if !errors.Is(err, domain.ErrProjectItemTemplate) {
		// 不符即停：带上实际错误。
		t.Fatalf("want ErrProjectItemTemplate, got %v", err)
	}
}
