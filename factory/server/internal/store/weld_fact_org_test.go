// 焊事实只绑人：组织按人现在的所属组织算，人改组织后旧事实跟着走；有人归在节点上就挡删节点。
package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/internal/store"
)

// 焊事实的组织取人现在的所属组织：改组织后旧事实按新组织筛，改回厂直属后只在直属里。
func TestWeldFactOrgFollowsPerson(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 建焊工，焊事实要挂在人身上。
	who, err := s.CreatePerson(ctx, "op-move", "焊工", "", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("person: %v", err)
	}
	// 建原来的工段。
	first, err := s.CreateOrgUnit(ctx, "一工段", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("unit first: %v", err)
	}
	// 建后来调去的工段。
	second, err := s.CreateOrgUnit(ctx, "二工段", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("unit second: %v", err)
	}
	// 先把人归到一工段。
	if err := s.SetPersonOrg(ctx, who.ID, &first.ID); err != nil {
		// 归组织失败就停测。
		t.Fatalf("set org first: %v", err)
	}
	// 落一条焊事实，事实里不带组织。
	fact := store.WeldFact{ID: id.New(), CreatorID: who.ID, LengthMM: 120, DurationSec: 12, OccurredAt: time.Now().UTC()}
	// 写入失败就停测。
	if _, err := s.MergeWeldFact(ctx, fact); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("merge: %v", err)
	}
	// 按一工段筛，应正好这一条且带一工段。
	rows, err := s.ListWeldFacts(ctx, store.WeldFactFilter{OrgUnitID: &first.ID})
	// 读失败、条数或组织不对都算失败。
	if err != nil || len(rows) != 1 || rows[0].OrgUnitID == nil || *rows[0].OrgUnitID != first.ID {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("first org rows: %+v %v", rows, err)
	}
	// 把人调到二工段。
	if err := s.SetPersonOrg(ctx, who.ID, &second.ID); err != nil {
		// 改组织失败就停测。
		t.Fatalf("set org second: %v", err)
	}
	// 按一工段再筛，旧事实已跟人走，应一条没有。
	if rows, err := s.ListWeldFacts(ctx, store.WeldFactFilter{OrgUnitID: &first.ID}); err != nil || len(rows) != 0 {
		// 旧事实还留在原组织就停测。
		t.Fatalf("first org after move: %+v %v", rows, err)
	}
	// 按二工段筛，旧事实应在这里。
	if rows, err := s.ListWeldFacts(ctx, store.WeldFactFilter{OrgUnitID: &second.ID}); err != nil || len(rows) != 1 || rows[0].ID != fact.ID {
		// 旧事实没跟到新组织就停测。
		t.Fatalf("second org after move: %+v %v", rows, err)
	}
	// 再改回厂直属。
	if err := s.SetPersonOrg(ctx, who.ID, nil); err != nil {
		// 改直属失败就停测。
		t.Fatalf("set org direct: %v", err)
	}
	// 只取厂直属，应正好这一条且组织为空。
	if rows, err := s.ListWeldFacts(ctx, store.WeldFactFilter{DirectOnly: true}); err != nil || len(rows) != 1 || rows[0].OrgUnitID != nil {
		// 直属里没有或还带组织就停测。
		t.Fatalf("direct rows: %+v %v", rows, err)
	}
}

// 有人归在节点上时删节点被拒；人挪走后才删得掉。
func TestPersonOrgBlocksDelete(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 建归在节点上的人。
	who, err := s.CreatePerson(ctx, "op-keep", "焊工", "", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("person: %v", err)
	}
	// 建要删的节点。
	unit, err := s.CreateOrgUnit(ctx, "在用工段", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("unit: %v", err)
	}
	// 把人归到这个节点。
	if err := s.SetPersonOrg(ctx, who.ID, &unit.ID); err != nil {
		// 归组织失败就停测。
		t.Fatalf("set org: %v", err)
	}
	// 有人归在节点上，删除必须被拒。
	if err := s.DeleteOrgUnit(ctx, unit.ID); !errors.Is(err, domain.ErrReferenced) {
		// 被放行删除就停测，人会指向不存在的节点。
		t.Fatalf("delete with person: %v", err)
	}
	// 把人挪回厂直属。
	if err := s.SetPersonOrg(ctx, who.ID, nil); err != nil {
		// 挪走失败就停测。
		t.Fatalf("clear org: %v", err)
	}
	// 没人归在节点上了，应能删掉。
	if err := s.DeleteOrgUnit(ctx, unit.ID); err != nil {
		// 删不掉就停测。
		t.Fatalf("delete after clear: %v", err)
	}
}
