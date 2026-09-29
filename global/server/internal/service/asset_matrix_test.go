// 阶段3第7圈：WAN 侧矩阵 1.3、4.1、4.3、4.4、5.2、7.2、9.2、10.1、10.3、12.1～12.2、13.6、14.1、16.1～16.2、18.1～18.2。
package service_test

import "testing"

// 阶段3要跑的矩阵编号，收尾用它查漏格。
var matrix03IDs = []string{
	"1.3",
	"4.1", "4.3", "4.4",
	"5.2",
	"7.2",
	"9.2",
	"10.1", "10.3", "10.4", "10.5",
	"12.1", "12.2",
	"13.6",
	"14.1",
	"16.1", "16.2",
	"18.1", "18.2",
}

// 把阶段3矩阵逐格跑完，漏号就失败。
func TestMatrix03(t *testing.T) {
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
		for _, id := range matrix03IDs {
			// 这个编号必须跑过，漏跑矩阵就不完整。
			if !ran[id] {
				// 报出没跑到的编号，漏格不能当成已覆盖。
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})
	// 跑身份与发布这组编号，失败落在对应格。
	testWANAssetIdentity(t, run)
	// 跑代建与读厂内正文，越权必须拒绝。
	testWANAssetAuthorship(t, run)
	// 跑升档和依赖这组编号，失败落在对应格。
	testWANAssetPromote(t, run)
}
