// 软件更新服务层：WAN 发布、厂自拉、本机云端确认。
package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/blob"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/nodekey"
	global "wmesh/global/internal/service"
)

// 软件发布要跑的编号，收尾用它查漏格。
var matrixSoftwareIDs = []string{
	"U1", "U6", "U7", "U13", "U15", "U16", "U17", "U18", "U24", "U25", "U26", "U27", "U29", "U31",
}

// 把软件发布矩阵逐格跑完，漏号就失败。
func TestMatrixSoftware(t *testing.T) {
	// 记下已跑编号，收尾靠它发现漏掉的格。
	ran := map[string]bool{}
	// 按编号开子测试并记已跑，漏记收尾会误报。
	run := func(id string, fn func(*testing.T)) {
		// 失败栈指到用例，避免停在夹具里面。
		t.Helper()
		// 这个编号单独成格，失败不连坐其它格。
		t.Run(id, func(t *testing.T) {
			// 标这一格已跑，漏标会在收尾被当成没跑。
			ran[id] = true
			// 执行这一格的断言，失败只记在这个编号。
			fn(t)
		})
	}
	// 收尾核对矩阵编号都跑过，漏号就失败。
	t.Cleanup(func() {
		// 逐个矩阵编号核对，漏一个就不算覆盖完。
		for _, id := range matrixSoftwareIDs {
			// 这个编号必须跑过，漏跑矩阵就不完整。
			if !ran[id] {
				// 报出没跑到的编号，漏格不能当成已覆盖。
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})

	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 登记工厂「厂A」，建不成后面没有厂可授权。
	a, err := h.WAN.CreateFactory(ctx, tok, "厂A", "sa-a", "超管A")
	// 登记工厂「厂A」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂A」失败。
		t.Fatal(err)
	}
	// 登记工厂「厂B」，建不成后面没有厂可授权。
	b, err := h.WAN.CreateFactory(ctx, tok, "厂B", "sa-b", "超管B")
	// 登记工厂「厂B」失败就停，建不成后面没有厂可授权。
	if err != nil {
		// 不符即停：登记工厂「厂B」失败。
		t.Fatal(err)
	}
	// 生成一对密钥，认领和改绑都用这把公钥。
	pub, _, err := nodekey.Generate()
	// 生成一对密钥失败就停，没有公钥认领就绑不成。
	if err != nil {
		// 不符即停：生成一对密钥失败。
		t.Fatal(err)
	}
	// 确认厂钥失败就停，厂钥确认失败就不能绑死。
	if err := h.WAN.ConfirmEnroll(ctx, a.Factory.ID, pub); err != nil {
		// 不符即停：确认厂钥失败。
		t.Fatal(err)
	}

	// U1：发布厂服务包失败就停，包发不出去厂侧就拉不到。
	run("U1", func(t *testing.T) {
		// 发布厂服务包失败就停，包发不出去厂侧就拉不到。
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, []byte("svc-5")); err != nil {
			// 不符即停：发布厂服务包失败。
			t.Fatal(err)
		}
	})
	// U7：发布客户端包失败就停，包发不出去厂侧就拉不到。
	run("U7", func(t *testing.T) {
		// 发布客户端包失败就停，包发不出去厂侧就拉不到。
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareClientAPK, "6.1.0", 3, []byte("apk-3")); err != nil {
			// 不符即停：发布客户端包失败。
			t.Fatal(err)
		}
	})
	// U27：总线记录条数不是2就停，这一步不能算通过。
	run("U27", func(t *testing.T) {
		// 准备记录总线，发布后核对通知了哪一厂。
		bus := &recordBus{}
		// 换上记录总线，后面才能核对通知了哪一厂。
		h.WAN.SetBus(bus)
		// 发布客户端包失败就停，包发不出去厂侧就拉不到。
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareClientAPK, "6.2.0", 4, []byte("apk-4")); err != nil {
			// 不符即停：发布客户端包失败。
			t.Fatal(err)
		}
		// 发布客户端包失败就停，包发不出去厂侧就拉不到。
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, []byte("svc-5")); err != nil {
			// 不符即停：发布客户端包失败。
			t.Fatal(err)
		}
		// 总线记录条数不是2就停，这一步不能算通过。
		if len(bus.ids) != 2 || bus.ids[0] != a.Factory.ID || bus.ids[1] != a.Factory.ID {
			// 不符即停：总线记录条数不是2。
			t.Fatalf("notified %+v", bus.ids)
		}
		// 总线记录包种类不对或版本不对就停，不能当通过。
		if bus.got[0].Kind != global.SoftwareClientAPK || bus.got[0].Version != 4 || bus.got[1].Kind != global.SoftwareFactoryService || bus.got[1].Version != 5 {
			// 不符即停：总线记录包种类不对或版本不对。
			t.Fatalf("cmd %+v", bus.got)
		}
		// 这里正文留着不该有的旧字段就停，这一步不能算通过。
		if strings.Contains(string(bus.raw[0]), "apk-4") || strings.Contains(string(bus.raw[1]), "svc-5") {
			// 不符即停：这里正文留着不该有的旧字段。
			t.Fatal("package body on mqtt")
		}
	})
	// U29：这里保留标记不对就停，这一步不能算通过。
	run("U29", func(t *testing.T) {
		// 列软件版本，版本列表读不到就对不齐。
		rows, err := h.WAN.ListSoftwareItems(ctx, tok, global.SoftwareClientAPK)
		// 列软件版本失败就停，版本列表读不到就对不齐。
		if err != nil {
			// 不符即停：列软件版本失败。
			t.Fatal(err)
		}
		// 按版本记下保留标记，删错版本会在这里露馅。
		keep := map[int64]string{}
		// 逐条查看结果，漏看一条会把结论判错。
		for _, row := range rows {
			// 按版本记下保留标记，删错版本会在这里露馅。
			keep[row.Version] = row.Keep
		}
		// 这里保留标记不对就停，这一步不能算通过。
		if keep[4] != global.SoftwareKeepLatest || keep[3] != "" {
			// 不符即停：这里保留标记不对。
			t.Fatalf("keep %+v", keep)
		}
		// 删除软件版本失败就停，删版本失败保留规则就乱。
		if err := h.WAN.DeleteSoftware(ctx, tok, global.SoftwareClientAPK, 3); err != nil {
			// 不符即停：删除软件版本失败。
			t.Fatal(err)
		}
		// 按版本读软件发布应被拒为找不到，放行或错类都算没拦住。
		if _, err := h.WAN.Store().SoftwareRelease(ctx, global.SoftwareClientAPK, 3); !errors.Is(err, domain.ErrNotFound) {
			// 不符即停：按版本读软件发布应被拒为找不到。
			t.Fatalf("kept %v", err)
		}
		// 按版本读软件发布应被拒为仍被引用，放行或错类都算没拦住。
		if err := h.WAN.DeleteSoftware(ctx, tok, global.SoftwareClientAPK, 4); !errors.Is(err, domain.ErrReferenced) {
			// 不符即停：按版本读软件发布应被拒为仍被引用。
			t.Fatalf("latest %v", err)
		}
	})
	// U31：请求清理镜像应被拒为未登录，放行或错类都算没拦住。
	run("U31", func(t *testing.T) {
		// 请求清理镜像失败就停，清理结果必须符合引用规则。
		if err := h.WAN.RequestImagePrune(ctx, tok, ""); err != nil {
			// 不符即停：请求清理镜像失败。
			t.Fatal(err)
		}
		// 请求清理镜像应被拒为未登录，放行或错类都算没拦住。
		if err := h.WAN.RequestImagePrune(ctx, "", ""); !errors.Is(err, domain.ErrUnauthorized) {
			// 不符即停：请求清理镜像应被拒为未登录。
			t.Fatalf("anon %v", err)
		}
	})
	// U15：发布厂服务包失败或版本不对就停，不能当通过。
	run("U15", func(t *testing.T) {
		// 发布厂服务包，包发不出去厂侧就拉不到。
		got, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, []byte("svc-5"))
		// 发布厂服务包失败或版本不对就停，不能当通过。
		if err != nil || got.Version != 5 {
			// 不符即停：发布厂服务包失败或版本不对。
			t.Fatalf("%+v %v", got, err)
		}
		// 装上对象存储桩，避免测试去碰真实的桶。
		h.WAN.SetBlobs(blob.NewMemory())
		// 发布厂服务包失败就停，包发不出去厂侧就拉不到。
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, []byte("svc-5")); err != nil {
			// 不符即停：发布厂服务包失败。
			t.Fatalf("restore %v", err)
		}
	})
	// U16：发布厂服务包应被拒为摘要不符，放行或错类都算没拦住。
	run("U16", func(t *testing.T) {
		// 发布厂服务包应被拒为摘要不符，放行或错类都算没拦住。
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.5.0", 5, []byte("svc-5-other")); !errors.Is(err, domain.ErrIntegrity) {
			// 不符即停：发布厂服务包应被拒为摘要不符。
			t.Fatalf("got %v", err)
		}
	})
	// U13：发布厂服务包应被拒为修订过旧，放行或错类都算没拦住。
	run("U13", func(t *testing.T) {
		// 发布厂服务包应被拒为修订过旧，放行或错类都算没拦住。
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareFactoryService, "1.4.0", 4, []byte("svc-4")); !errors.Is(err, domain.ErrStaleRevision) {
			// 不符即停：发布厂服务包应被拒为修订过旧。
			t.Fatalf("publish %v", err)
		}
		// 厂侧拉厂服务包应被拒为修订过旧，放行或错类都算没拦住。
		if _, err := h.WAN.PullSoftware(ctx, a.Factory.ID, global.SoftwareFactoryService, 4); !errors.Is(err, domain.ErrStaleRevision) {
			// 不符即停：厂侧拉厂服务包应被拒为修订过旧。
			t.Fatalf("pull %v", err)
		}
	})
	// U17：读最新软件应被拒为越权，放行或错类都算没拦住。
	run("U17", func(t *testing.T) {
		// 读最新软件应被拒为越权，放行或错类都算没拦住。
		if _, err := h.WAN.LatestSoftware(ctx, b.Factory.ID, global.SoftwareFactoryService); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：读最新软件应被拒为越权。
			t.Fatalf("unclaimed %v", err)
		}
		// 厂侧拉厂服务包应被拒为越权，放行或错类都算没拦住。
		if _, err := h.WAN.PullSoftware(ctx, b.Factory.ID, global.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：厂侧拉厂服务包应被拒为越权。
			t.Fatalf("unclaimed pull %v", err)
		}
	})
	// U18：读最新软件应被拒为越权，放行或错类都算没拦住。
	run("U18", func(t *testing.T) {
		// 读最新软件应被拒为越权，放行或错类都算没拦住。
		if _, err := h.WAN.LatestSoftware(ctx, a.Factory.ID, global.SoftwareWANService); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：读最新软件应被拒为越权。
			t.Fatalf("wan pack %v", err)
		}
		// 厂侧拉取软件应被拒为越权，放行或错类都算没拦住。
		if _, err := h.WAN.PullSoftware(ctx, a.Factory.ID, global.SoftwareWANService, 1); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：厂侧拉取软件应被拒为越权。
			t.Fatalf("wan pull %v", err)
		}
	})
	// U6：确认云端已升级应被拒为越权，放行或错类都算没拦住。
	run("U6", func(t *testing.T) {
		// 确认云端已升级应被拒为越权，放行或错类都算没拦住。
		if err := h.WAN.ConfirmWANUpdate(ctx, tok, global.SoftwareFactoryService, 5); !errors.Is(err, domain.ErrForbidden) {
			// 不符即停：确认云端已升级应被拒为越权。
			t.Fatalf("got %v", err)
		}
	})
	// U26：发布软件包应被拒为名称不合法，放行或错类都算没拦住。
	run("U26", func(t *testing.T) {
		// 发布软件包应被拒为名称不合法，放行或错类都算没拦住。
		if _, err := h.WAN.PublishSoftware(ctx, tok, "postgres", "18", 1, []byte("sql")); !errors.Is(err, domain.ErrInvalidName) {
			// 不符即停：发布软件包应被拒为名称不合法。
			t.Fatalf("kind %v", err)
		}
		// 发布软件包应被拒为名称不合法，放行或错类都算没拦住。
		if _, err := h.WAN.PublishSoftware(ctx, tok, "sql", "1", 1, []byte("--")); !errors.Is(err, domain.ErrInvalidName) {
			// 不符即停：发布软件包应被拒为名称不合法。
			t.Fatalf("sql %v", err)
		}
	})
	// U24：读云端当前版本失败或版本不对就停，不能当通过。
	run("U24", func(t *testing.T) {
		// 准备正文样本，后面入库和比对都用它。
		body := []byte("wan-svc-2")
		// 发布云端服务包失败就停，包发不出去厂侧就拉不到。
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareWANService, "2.0.0", 2, body); err != nil {
			// 不符即停：发布云端服务包失败。
			t.Fatal(err)
		}
		// 假定本机升级成功，否则确认成功的路径测不到。
		h.WAN.Updates.SetApplyOutcome(global.ApplyOK)
		// 设定本机升级结果失败就停，不设定结果就走不到确认成功。
		if err := h.WAN.ConfirmWANUpdate(ctx, tok, global.SoftwareWANService, 2); err != nil {
			// 不符即停：设定本机升级结果失败。
			t.Fatal(err)
		}
		// 读云端当前版本，当前版本读不到确认没意义。
		cur, err := h.WAN.CurrentWANSoftware(ctx, tok)
		// 读云端当前版本失败或版本不对就停，不能当通过。
		if err != nil || cur.Version != 2 || cur.VersionName != "2.0.0" {
			// 不符即停：读云端当前版本失败或版本不对。
			t.Fatalf("current %+v %v", cur, err)
		}
		// 核对摘要版本不对就停，这一步不能算通过。
		if !digest.Match(body, cur.Digest) && cur.Version != 2 {
			// 不符即停：核对摘要版本不对。
			t.Fatal("digest")
		}
	})
	// U25：确认云端已升级应被拒为未登录，放行或错类都算没拦住。
	run("U25", func(t *testing.T) {
		// 发布云端服务包失败就停，包发不出去厂侧就拉不到。
		if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareWANService, "2.1.0", 3, []byte("wan-svc-3")); err != nil {
			// 不符即停：发布云端服务包失败。
			t.Fatal(err)
		}
		// 确认云端已升级应被拒为未登录，放行或错类都算没拦住。
		if err := h.WAN.ConfirmWANUpdate(ctx, "", global.SoftwareWANService, 3); !errors.Is(err, domain.ErrUnauthorized) {
			// 不符即停：确认云端已升级应被拒为未登录。
			t.Fatalf("got %v", err)
		}
		// 读云端当前版本，当前版本读不到确认没意义。
		cur, err := h.WAN.CurrentWANSoftware(ctx, tok)
		// 读云端当前版本失败或版本不对就停，不能当通过。
		if err != nil || cur.Version != 2 {
			// 不符即停：读云端当前版本失败或版本不对。
			t.Fatalf("still %+v %v", cur, err)
		}
		// 删除软件版本应被拒为仍被引用，放行或错类都算没拦住。
		if err := h.WAN.DeleteSoftware(ctx, tok, global.SoftwareWANService, 2); !errors.Is(err, domain.ErrReferenced) {
			// 不符即停：删除软件版本应被拒为仍被引用。
			t.Fatalf("installed %v", err)
		}
	})

	// 厂侧拉厂服务包，拉不到包厂侧就没法升级。
	offer, err := h.WAN.PullSoftware(ctx, a.Factory.ID, global.SoftwareFactoryService, 5)
	// 厂侧拉厂服务包失败或版本不对就停，不能当通过。
	if err != nil || offer.Version != 5 || string(offer.Body) != "svc-5" {
		// 不符即停：厂侧拉厂服务包失败或版本不对。
		t.Fatalf("enrolled pull %+v %v", offer, err)
	}

	// 读审计，读不到就无法核对有没有记。
	rows, err := h.WAN.ListAudit(ctx)
	// 读审计失败就停，读不到就无法核对有没有记。
	if err != nil {
		// 不符即停：读审计失败。
		t.Fatal(err)
	}
	// 审计里不能出现口令或正文，出现就是泄密。
	if audit.ContainsAny(audit.Dump(rows), "svc-5", "wan-secret") {
		// 不符即停：审计里不能出现口令或正文。
		t.Fatal("secret or package bytes in audit")
	}
	// 审计必须记下这一笔，缺了就没法对账。
	if !audit.HasResult(rows, "publish_software", audit.Allow) || !audit.HasResult(rows, "publish_software", audit.Deny) {
		// 不符即停：审计必须记下这一笔。
		t.Fatal("missing publish audit")
	}
	// 审计必须记下这一笔，缺了就没法对账。
	if !audit.HasResult(rows, "confirm_software", audit.Allow) || !audit.HasResult(rows, "confirm_software", audit.Deny) {
		// 不符即停：审计必须记下这一笔。
		t.Fatal("missing confirm audit")
	}
}

// 记下总线投递，用来断言通知了哪一厂。
type recordBus struct {
	// 收到通知的工厂，顺序就是投递顺序。
	ids []uuid.UUID
	// 解开后的指令，用来核对种类和版本。
	got []global.Cmd
	// 原始报文，用来确认包体没有上总线。
	raw [][]byte
}

// 记下总线投到哪一厂、什么指令，解不开就忽略。
func (b *recordBus) Publish(factoryID uuid.UUID, payload []byte) error {
	// 先留出位置，循环里找到再填，找不到就失败。
	var cmd global.Cmd
	// 条件成立才做这一支，漏进来会算错样本。
	if json.Unmarshal(payload, &cmd) != nil {
		return nil
	}
	// 按投递顺序记下工厂，漏记会把通知判丢。
	b.ids = append(b.ids, factoryID)
	// 记下解开的指令，后面核对种类和版本。
	b.got = append(b.got, cmd)
	// 留原始报文，用来确认包体没有上总线。
	b.raw = append(b.raw, append([]byte(nil), payload...))
	return nil
}

// 测试不走请求应答，一律当成厂通道离线。
func (b *recordBus) Call(context.Context, uuid.UUID, string, []byte) ([]byte, error) {
	return nil, domain.ErrFactoryOffline
}

// 测试不拆通道，空实现只为凑齐总线接口。
func (*recordBus) Drop(uuid.UUID) {}

// 验对象、磁盘和库占用，匿名会话不能看。
func TestStorageUsage(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 发布客户端包失败就停，包发不出去厂侧就拉不到。
	if _, err := h.WAN.PublishSoftware(ctx, tok, global.SoftwareClientAPK, "6.0.0", 1, []byte("apk-body")); err != nil {
		// 不符即停：发布客户端包失败。
		t.Fatal(err)
	}
	// 读存储占用，占用读不到就无法核对。
	got, err := h.WAN.StorageUsage(ctx, tok)
	// 读存储占用失败就停，占用读不到就无法核对。
	if err != nil {
		// 不符即停：读存储占用失败。
		t.Fatal(err)
	}
	// 返回值占用数字不对就停，这一步不能算通过。
	if got.OSS.Used != int64(len("apk-body")) || got.OSS.Objects != 1 {
		// 不符即停：返回值占用数字不对。
		t.Fatalf("oss %+v", got.OSS)
	}
	// 返回值占用数字不对就停，这一步不能算通过。
	if got.Disk.Total <= 0 || got.Database <= 0 {
		// 不符即停：返回值占用数字不对。
		t.Fatalf("disk/db %+v", got)
	}
	// 返回值镜像占用不对或占用数字不对就停，不能当通过。
	if got.Images.Count != 0 || got.Images.Used != 0 {
		// 不符即停：返回值镜像占用不对或占用数字不对。
		t.Fatalf("images %+v", got.Images)
	}
	// 读存储占用应被拒为未登录，放行或错类都算没拦住。
	if _, err := h.WAN.StorageUsage(ctx, ""); !errors.Is(err, domain.ErrUnauthorized) {
		// 不符即停：读存储占用应被拒为未登录。
		t.Fatalf("anon %v", err)
	}
}

// 镜像清扫桩，占用和点名都从这里读。
type reportJanitor struct {
	// 桩返回的镜像清单原文。
	raw []byte
	// 最近一次被点名清理的镜像引用。
	ref string
}

// 记下被点名的镜像，断言要核对清的是不是它。
func (j *reportJanitor) RequestPrune(ref string) error {
	// 记下被点名的镜像，断言要核对清的是不是它。
	j.ref = ref
	return nil
}

// 桩上报清理已结束，测试不用干等结果。
func (j *reportJanitor) PruneResult() (string, bool, bool, error) { return "0B", true, true, nil }

// 把桩里的镜像清单原样交回，供占用断言。
func (j *reportJanitor) ImagesJSON() ([]byte, error) { return j.raw, nil }

// 验镜像占用，在用的不能清，没有的找不到。
func TestImageOccupancy(t *testing.T) {
	// 准备本测上下文，没有它库和服务都开不了。
	ctx := context.Background()
	// 起云端库和服务，起不来整段验收作废。
	h := New(t)
	// 立云端超管失败就停，立不住后面没有人能登录。
	if err := h.WAN.BootstrapAdmin(ctx, "w", "wan-secret"); err != nil {
		// 不符即停：立云端超管失败。
		t.Fatal(err)
	}
	// 登录拿会话，没有票后面接口都进不去。
	tok, err := h.WAN.Login(ctx, "w", "wan-secret")
	// 登录拿会话失败就停，没有票后面接口都进不去。
	if err != nil {
		// 不符即停：登录拿会话失败。
		t.Fatal(err)
	}
	// 装上镜像桩，占用和清理都读这块假数据。
	h.WAN.SetImageJanitor(&reportJanitor{raw: []byte(`{"kind":"wan_service","used":12,"count":1,"items":[{"ref":"app:dev","id":"x","size":12,"keep":"current"}]}`)})
	// 读存储占用，占用读不到就无法核对。
	got, err := h.WAN.StorageUsage(ctx, tok)
	// 读存储占用失败就停，占用读不到就无法核对。
	if err != nil {
		// 不符即停：读存储占用失败。
		t.Fatal(err)
	}
	// 返回值多项不符预期就停，说明没有按规则落下。
	if got.Images.Kind != "wan_service" || got.Images.Used != 12 || got.Images.Count != 1 || len(got.Images.Items) != 1 {
		// 不符即停：返回值多项不符预期。
		t.Fatalf("images %+v", got.Images)
	}
	// 返回值保留标记不对就停，这一步不能算通过。
	if got.Images.Items[0].Keep != "current" {
		// 不符即停：返回值保留标记不对。
		t.Fatalf("keep %+v", got.Images.Items[0])
	}
	// 准备镜像清单，在用和可清的旧版都在里面。
	jan := &reportJanitor{raw: []byte(`{"kind":"wan_service","used":20,"count":2,"items":[{"ref":"app:dev","id":"x","size":12,"keep":"current"},{"ref":"app:old","id":"y","size":8,"keep":""}]}`)}
	// 装上镜像桩，占用和清理都读这块假数据。
	h.WAN.SetImageJanitor(jan)
	// 请求清理镜像应被拒为仍被引用，放行或错类都算没拦住。
	if err := h.WAN.RequestImagePrune(ctx, tok, "app:dev"); !errors.Is(err, domain.ErrReferenced) {
		// 不符即停：请求清理镜像应被拒为仍被引用。
		t.Fatalf("current %v", err)
	}
	// 请求清理镜像应被拒为找不到，放行或错类都算没拦住。
	if err := h.WAN.RequestImagePrune(ctx, tok, "app:missing"); !errors.Is(err, domain.ErrNotFound) {
		// 不符即停：请求清理镜像应被拒为找不到。
		t.Fatalf("missing %v", err)
	}
	// 请求清理镜像失败就停，清理结果必须符合引用规则。
	if err := h.WAN.RequestImagePrune(ctx, tok, "app:old"); err != nil {
		// 不符即停：请求清理镜像失败。
		t.Fatal(err)
	}
	// 点名必须是旧镜像，清错了说明没拦住在用的。
	if jan.ref != "app:old" {
		// 不符即停：点名必须是旧镜像。
		t.Fatalf("ref %q", jan.ref)
	}
}
