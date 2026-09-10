// 验收 WAN 侧应用服务，不连厂库。
package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/id"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// stubBoot 只给 WAN 一个人员身份，不写厂库。
type stubBoot struct{}

func (stubBoot) Bootstrap(_ context.Context, _ uuid.UUID, _, _ string) (uuid.UUID, string, error) {
	return id.New(), "fixture-activation", nil
}

type Harness struct {
	t   *testing.T
	WAN *service.Service
}

func New(t *testing.T) *Harness {
	t.Helper()
	admin := testpg.Open(t)
	_, wanDSN := testpg.CreateDB(t, admin, "wmesh_wan")
	return &Harness{t: t, WAN: service.NewService(store.Open(testpg.OpenMigrated(t, wanDSN)), stubBoot{})}
}
