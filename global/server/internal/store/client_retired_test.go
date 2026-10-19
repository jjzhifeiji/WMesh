// 库层绑定与改分在事务里复查目标厂：已注销的拒绝，停用的照常。
package store_test

import (
	"context"
	"errors"
	"testing"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/store"
)

// 验绕过服务直接绑定或改分到已注销厂也被库层拒绝，归属和修订不动；停用厂照常可分。
func TestBindClientRetiredFactory(t *testing.T) {
	// 夹具不用取消，这一段测完就丢掉。
	ctx := context.Background()
	// 开一块已迁移的空库，名录和设备都写进这里。
	s := store.Open(testpg.Fresh(t))
	// 登记厂R，稍后注销。
	r, err := s.RegisterFactory(ctx, id.New(), "厂R", id.New(), "sa-r", "超管R", "enroll-r")
	// 厂没登记上就停，没有可注销的厂。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 登记厂B，一直有效，作对照。
	b, err := s.RegisterFactory(ctx, id.New(), "厂B", id.New(), "sa-b", "超管B", "enroll-b")
	// 厂没登记上就停，没有对照厂。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 登记厂D，稍后停用。
	d, err := s.RegisterFactory(ctx, id.New(), "厂D", id.New(), "sa-d", "超管D", "enroll-d")
	// 厂没登记上就停，没有可停用的厂。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 注销厂R，后面绑定和改分都拿它当目标。
	if _, _, err := s.SetFactoryStatus(ctx, r.ID, store.FactoryRetired); err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 停用厂D，按口径停用的照常可分。
	if _, _, err := s.SetFactoryStatus(ctx, d.ID, store.FactoryDisabled); err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 登记一台未分配设备，拿来绑定和改分。
	c, err := s.CreateClient(ctx, id.New(), "焊机", nil, "")
	// 设备没登记上就停，没有对象。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 绑到已注销厂，库层必须按已注销拒绝。
	if _, err := s.BindClient(ctx, c.ID, r.ID); !errors.Is(err, domain.ErrFactoryRetired) {
		// 带上错误停测。
		t.Fatalf("bind to retired: %v", err)
	}
	// 读回设备，核对仍未分配、修订仍是零。
	cur, err := s.ClientByID(ctx, c.ID)
	// 读失败或已被分走都不对。
	if err != nil || cur.FactoryID != nil || cur.BindingRevision != 0 {
		// 带上当前行停测。
		t.Fatalf("bind to retired changed row: %+v %v", cur, err)
	}
	// 绑到有效的厂B，作改分起点。
	if _, err := s.BindClient(ctx, c.ID, b.ID); err != nil {
		// 带上错误停测。
		t.Fatalf("bind to active: %v", err)
	}
	// 改到已注销厂，库层必须按已注销拒绝。
	if _, _, err := s.RebindClient(ctx, c.ID, r.ID); !errors.Is(err, domain.ErrFactoryRetired) {
		// 带上错误停测。
		t.Fatalf("rebind to retired: %v", err)
	}
	// 读回设备，核对仍属厂B、修订仍是一。
	cur, err = s.ClientByID(ctx, c.ID)
	// 读失败、归属变了或修订升了都不对。
	if err != nil || cur.FactoryID == nil || *cur.FactoryID != b.ID || cur.BindingRevision != 1 {
		// 带上当前行停测。
		t.Fatalf("rebind to retired changed row: %+v %v", cur, err)
	}
	// 改到停用的厂D，应照常成功且修订升到二。
	moved, _, err := s.RebindClient(ctx, c.ID, d.ID)
	// 改分失败、归属不对或修订没升都不对。
	if err != nil || moved.FactoryID == nil || *moved.FactoryID != d.ID || moved.BindingRevision != 2 {
		// 带上结果停测。
		t.Fatalf("rebind to disabled: %+v %v", moved, err)
	}
	// 再登记一台，验绑到停用厂也照常。
	c2, err := s.CreateClient(ctx, id.New(), "焊机2", nil, "")
	// 设备没登记上就停，没有对象。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 绑到停用的厂D，应照常成功。
	if _, err := s.BindClient(ctx, c2.ID, d.ID); err != nil {
		// 带上错误停测。
		t.Fatalf("bind to disabled: %v", err)
	}
	// 再登记一台，验绑到不在名录的厂。
	c3, err := s.CreateClient(ctx, id.New(), "焊机3", nil, "")
	// 设备没登记上就停，没有对象。
	if err != nil {
		// 带上原因停测。
		t.Fatal(err)
	}
	// 目标厂不在名录，必须按不存在拒绝。
	if _, err := s.BindClient(ctx, c3.ID, id.New()); !errors.Is(err, domain.ErrNotFound) {
		// 带上错误停测。
		t.Fatalf("bind to missing: %v", err)
	}
}
