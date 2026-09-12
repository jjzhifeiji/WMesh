// 验收 WAN 侧应用服务，不连厂库。
package service_test

import (
	"testing"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

func applyProcess(raw []byte) []byte {
	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProcess))
	if err != nil {
		panic(err)
	}
	out, err := contenttpl.Apply(schema, raw)
	if err != nil {
		panic(err)
	}
	return out
}

func applyProject(raw []byte) []byte {
	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProject))
	if err != nil {
		panic(err)
	}
	out, err := contenttpl.Apply(schema, raw)
	if err != nil {
		panic(err)
	}
	return out
}

type Harness struct {
	t   *testing.T
	WAN *service.Service
}

func New(t *testing.T) *Harness {
	t.Helper()
	admin := testpg.Open(t)
	_, wanDSN := testpg.CreateDB(t, admin, "wmesh_wan")
	return &Harness{t: t, WAN: service.NewService(store.Open(testpg.OpenMigrated(t, wanDSN)))}
}
