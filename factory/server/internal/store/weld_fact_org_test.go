// 焊事实发生节点按快照存：节点删了也能收下离线旧事实，已到库的事实照样挡删节点。
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

// 示教器离线时节点被删，回连上送这个节点的焊事实应能收下、重发幂等并能读回快照。
func TestWeldFactKeepsDeletedOrgSnapshot(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 建当时登录的焊工，焊事实要挂在人身上。
	who, err := s.CreatePerson(ctx, "op-gone", "焊工", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("person: %v", err)
	}
	// 建一个从没被引用过的节点，后面把它删掉。
	gone, err := s.CreateOrgUnit(ctx, "已撤工段", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("unit: %v", err)
	}
	// 示教器离线期间节点在厂端被物理删除。
	if err := s.DeleteOrgUnit(ctx, gone.ID); err != nil {
		// 没被引用的节点应能删掉，删不掉就没有前提。
		t.Fatalf("delete unit: %v", err)
	}
	// 另拷一份节点身份再取址，事实里存的是快照。
	unitID := gone.ID
	// 组装示教器本机留着的旧事实，节点和路径都是删前快照。
	fact := store.WeldFact{
		ID: id.New(), CreatorID: who.ID, OrgUnitID: &unitID,
		OrgPath:  []store.PathNode{{ID: gone.ID, Name: gone.Name}},
		LengthMM: 120, DurationSec: 12, OccurredAt: time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC),
	}
	// 回连上送这条事实，节点已删也必须收下。
	if _, err := s.MergeWeldFact(ctx, fact); err != nil {
		// 被外键挡成找不到就是问题复现，停测。
		t.Fatalf("merge after unit deleted: %v", err)
	}
	// 示教器没收到回包会重发，同一条再送应幂等收下。
	if _, err := s.MergeWeldFact(ctx, fact); err != nil {
		// 重发被拒会让示教器一直重试，停测。
		t.Fatalf("merge retry: %v", err)
	}
	// 按产生端身份读回，节点身份和路径快照都要原样保留。
	got, err := s.WeldFactByID(ctx, fact.ID)
	// 读回失败、节点身份丢了或路径快照不对，都算失败。
	if err != nil || got.OrgUnitID == nil || *got.OrgUnitID != gone.ID || len(got.OrgPath) != 1 || got.OrgPath[0].ID != gone.ID || got.OrgPath[0].Name != gone.Name {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("read back: %+v %v", got, err)
	}
}

// 去掉外键后，路径里提到节点的焊事实仍要挡住删除，节点只能停用。
func TestWeldFactOrgPathStillBlocksDelete(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 建当时登录的焊工，焊事实要挂在人身上。
	who, err := s.CreatePerson(ctx, "op-keep", "焊工", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("person: %v", err)
	}
	// 建焊事实要落的节点。
	unit, err := s.CreateOrgUnit(ctx, "在用工段", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("unit: %v", err)
	}
	// 按当前树冻结路径，和示教器上送的快照同形。
	path, err := s.PathSnapshot(ctx, unit.ID)
	// 冻结路径失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("path: %v", err)
	}
	// 另拷一份节点身份再取址，事实里存的是快照。
	unitID := unit.ID
	// 在这个节点上落一条焊事实。
	if _, err := s.MergeWeldFact(ctx, store.WeldFact{ID: id.New(), CreatorID: who.ID, OrgUnitID: &unitID, OrgPath: path, LengthMM: 80, DurationSec: 8, OccurredAt: time.Now().UTC()}); err != nil {
		// 节点在用时写入不该失败，失败就停测。
		t.Fatalf("merge: %v", err)
	}
	// 有焊事实提到这个节点时删除必须被拒。
	if err := s.DeleteOrgUnit(ctx, unit.ID); !errors.Is(err, domain.ErrReferenced) {
		// 没外键后被放行删除就停测。
		t.Fatalf("delete referenced by weld fact: %v", err)
	}
}

// 路径快照为空、只带节点身份的焊事实，也要挡住删除，免得事实指向不存在的节点。
func TestWeldFactOrgIDBlocksDeleteWithoutPath(t *testing.T) {
	// 测试没有取消信号，用空白上下文即可。
	ctx := context.Background()
	// 单独起一套厂库，免得用例互相踩到。
	facDB, facID := testpg.Fresh(t)
	// 按这家厂的稳定身份打开库。
	s := store.Open(facDB, facID)
	// 建当时登录的焊工，焊事实要挂在人身上。
	who, err := s.CreatePerson(ctx, "op-nopath", "焊工", false)
	// 建账号失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("person: %v", err)
	}
	// 建焊事实要落的节点。
	unit, err := s.CreateOrgUnit(ctx, "空路径工段", nil)
	// 建组织节点失败就停测，避免后面没有前提。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("unit: %v", err)
	}
	// 另拷一份节点身份再取址，事实里存的是快照。
	unitID := unit.ID
	// 示教器上送的路径快照默认是空列表，只带节点身份。
	fact := store.WeldFact{ID: id.New(), CreatorID: who.ID, OrgUnitID: &unitID, OrgPath: []store.PathNode{}, LengthMM: 60, DurationSec: 6, OccurredAt: time.Now().UTC()}
	// 写入失败就停测，节点在用时不该被拒。
	if _, err := s.MergeWeldFact(ctx, fact); err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("merge: %v", err)
	}
	// 路径里没提到节点，单凭节点身份也必须挡删。
	if err := s.DeleteOrgUnit(ctx, unit.ID); !errors.Is(err, domain.ErrReferenced) {
		// 被放行删除就停测，事实会指向不存在的节点。
		t.Fatalf("delete referenced by weld fact org id: %v", err)
	}
	// 节点应还在，被拒的删除不能落库。
	if _, err := s.PathSnapshot(ctx, unit.ID); err != nil {
		// 节点没了说明拒绝后仍删了行。
		t.Fatalf("unit gone after reject: %v", err)
	}
}
