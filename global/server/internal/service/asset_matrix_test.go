// 阶段3第7圈：WAN 侧矩阵 1.3、4.1、4.3、4.4、5.2、7.2、9.2、10.1、10.3、12.1～12.2、13.6、14.1、16.1～16.2、18.1～18.2。
package service_test

import "testing"

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

func TestMatrix03(t *testing.T) {
	ran := map[string]bool{}
	run := func(id string, fn func(*testing.T)) {
		t.Helper()
		t.Run(id, func(t *testing.T) {
			ran[id] = true
			fn(t)
		})
	}
	t.Cleanup(func() {
		for _, id := range matrix03IDs {
			if !ran[id] {
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})
	testWANAssetIdentity(t, run)
	testWANAssetAuthorship(t, run)
	testWANAssetPromote(t, run)
}
