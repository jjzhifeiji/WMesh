// 阶段3第7圈：厂内侧矩阵 1.1～18.2。
package service_test

import "testing"

var matrix03IDs = []string{
	"1.1", "1.2", "1.3",
	"2.1", "2.2", "2.3",
	"3.1", "3.2", "3.3", "3.4", "3.5", "3.6", "3.7",
	"4.2",
	"5.1", "5.2", "5.3", "5.4",
	"6.1", "6.2", "6.3",
	"7.1", "7.2", "7.3",
	"8.1", "8.2", "8.3", "8.4", "8.5",
	"9.1", "9.2", "9.3",
	"10.2",
	"11.1", "11.2", "11.3",
	"13.1", "13.2", "13.3", "13.4", "13.5",
	"14.2", "14.3",
	"15.1", "15.2",
	"16.1", "16.2",
	"17.1", "17.2", "17.3",
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
	testAssetIdentity(t, run)
	testAssetAuthorship(t, run)
	testAssetPromote(t, run)
}
