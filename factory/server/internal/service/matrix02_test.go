// 阶段2第7圈：厂内侧矩阵 1.1～16.3。
package service_test

import "testing"

var matrix02IDs = []string{
	"1.1", "1.2",
	"2.1", "2.2", "2.3",
	"3.1", "3.2",
	"4.1", "4.2",
	"5.1", "5.2",
	"6.1",
	"7.1", "7.2",
	"8.1",
	"9.1", "9.2",
	"10.1",
	"11.1",
	"12.1", "12.2", "12.3", "12.4",
	"13.1", "13.2", "13.3", "13.4",
	"14.1", "14.2", "14.3",
	"15.1",
	"16.1", "16.2", "16.3",
}

func TestMatrix02(t *testing.T) {
	ran := map[string]bool{}
	run := func(id string, fn func(*testing.T)) {
		t.Helper()
		t.Run(id, func(t *testing.T) {
			ran[id] = true
			fn(t)
		})
	}
	t.Cleanup(func() {
		for _, id := range matrix02IDs {
			if !ran[id] {
				t.Errorf("矩阵编号未跑：%s", id)
			}
		}
	})
	testNodeMatrix(t, run)
	testPersonMatrix(t, run)
	testConvergeMatrix(t, run)
}
