package httpapi

import (
	"testing"
	"time"
)

// 补报原时刻：窗口内照收，空、非法、超前、过旧都按现在记。
func TestBackfillAt(t *testing.T) {
	// 固定现在，免得用例随时钟漂。
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	// 一小时前的补报应原样收下。
	got := backfillAt("2026-11-03T19:00:00+08:00", now)
	// 没收下或时刻不对就失败。
	if got == nil || !got.Equal(now.Add(-time.Hour)) {
		// 原时刻没按补报收下，停测。
		t.Fatalf("一小时前的补报应收下：%v", got)
	}
	// 逐个看不该收的原时刻。
	for _, raw := range []string{"", "  ", "昨天", "2026-11-03T12:05:00Z", "2026-10-01T00:00:00Z"} {
		// 这些都应当没传处理。
		if at := backfillAt(raw, now); at != nil {
			// 不该收的被收下了，停测。
			t.Fatalf("%q 不该收下，得到 %v", raw, at)
		}
	}
}
