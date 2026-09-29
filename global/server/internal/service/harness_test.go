// 验收 WAN 侧应用服务，不连厂库。
package service_test

import (
	"testing"

	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
)

// 按工艺字段表套正文，套坏了夹具直接停。
func applyProcess(raw []byte) []byte {
	// 导出字段表，导不出就没法套正文。
	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProcess))
	// 导出字段表失败就停，导不出就没法套正文。
	if err != nil {
		// 夹具出错就停，字段表坏了不能继续套。
		panic(err)
	}
	// 按字段表套正文，套失败说明样本不合法。
	out, err := contenttpl.Apply(schema, raw)
	// 按字段表套正文失败就停，套失败说明样本不合法。
	if err != nil {
		// 夹具出错就停，字段表坏了不能继续套。
		panic(err)
	}
	return out
}

// 按工程字段表套正文，套坏了夹具直接停。
func applyProject(raw []byte) []byte {
	// 导出字段表，导不出就没法套正文。
	schema, err := contenttpl.Marshal(contenttpl.Default(contenttpl.KindProject))
	// 导出字段表失败就停，导不出就没法套正文。
	if err != nil {
		// 夹具出错就停，字段表坏了不能继续套。
		panic(err)
	}
	// 按字段表套正文，套失败说明样本不合法。
	out, err := contenttpl.Apply(schema, raw)
	// 按字段表套正文失败就停，套失败说明样本不合法。
	if err != nil {
		// 夹具出错就停，字段表坏了不能继续套。
		panic(err)
	}
	return out
}

// 云端验收夹具，只握服务，不连厂库。
type Harness struct {
	// 当前用例，失败打在这一格上。
	t *testing.T
	// 被测的云端应用服务。
	WAN *service.Service
}

// 开一块已迁移的云端库，并接上应用服务。
func New(t *testing.T) *Harness {
	// 失败栈指到用例，避免停在夹具里面。
	t.Helper()
	// 连上测试库，连不上就没有云端可验。
	admin := testpg.Open(t)
	// 建一块云端库，建不成迁移没有地方跑。
	_, wanDSN := testpg.CreateDB(t, admin, "wmesh_wan")
	// 迁移并接上服务，任一步失败夹具不能用。
	return &Harness{t: t, WAN: service.NewService(store.Open(testpg.OpenMigrated(t, wanDSN)))}
}
