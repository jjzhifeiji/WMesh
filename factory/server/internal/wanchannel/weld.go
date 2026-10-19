package wanchannel

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/google/uuid"
)

const weldSkipsLogged = 5 // 告警里最多带几条跳过原因

// WeldSummaryRow 是上送 WAN 的一粒焊汇总，不含人员组织。
type WeldSummaryRow struct {
	Day         string `json:"day"`         // UTC 日期 YYYY-MM-DD
	ProjectName string `json:"projectName"` // 工程名快照或未关联
	WeldKind    string `json:"weldKind"`    // 焊接模式
	RunCount    int64  `json:"runCount"`    // 该粒次数
	LengthMM    int64  `json:"lengthMm"`    // 该粒焊长毫米
	DurationSec int64  `json:"durationSec"` // 该粒时长秒
}

// 上送只装汇总行，避免把人员组织带出去。
type weldSummariesBody struct {
	Rows []WeldSummaryRow `json:"rows"` // 该厂当前全量汇总
}

// WAN 收下整表后的回执；旧版 WAN 回 204 没有正文，保持零值。
type weldSummariesReceipt struct {
	Skipped      []weldSummarySkip `json:"skipped"`      // 前若干条被跳过的行
	SkippedCount int               `json:"skippedCount"` // 被跳过的总行数
}

// WAN 跳过的一行：在上送里的序号和英文原因。
type weldSummarySkip struct {
	Index  int    `json:"index"`  // 该行在上送里的序号，从 0 起
	Reason string `json:"reason"` // 跳过的英文原因
}

// PostWeldSummaries 用厂钥把当前焊汇总 POST 给 WAN；被跳过的行只记告警。
func PostWeldSummaries(ctx context.Context, wanHTTP string, factoryID uuid.UUID, priv []byte, rows []WeldSummaryRow) error {
	// 用厂钥签这次上送，签不出平台就不会收。
	p := newPuller(wanHTTP, factoryID, priv)
	// 没有记录也要送空表，避免对端把缺省当成没送。
	if rows == nil {
		// 空表让平台清掉旧汇总，而不是保持上一份。
		rows = []WeldSummaryRow{}
	}
	// 接住回执，看平台跳过了哪些行。
	var receipt weldSummariesReceipt
	// 签完再送出，平台拒绝则整次上送失败。
	if err := p.postJSON(ctx, "/v1/channel/weld-summaries", weldSummariesBody{Rows: rows}, &receipt); err != nil {
		return err
	}
	// 总数和列表取大的，回执不齐也不漏报。
	skipped := max(receipt.SkippedCount, len(receipt.Skipped))
	// 有行被跳过就记告警，带上条数和前几条原因。
	if skipped > 0 {
		// 告警带条数和前几条原因，整次上送仍算成功。
		slog.Warn("wan skipped weld summary rows", "skipped", skipped, "first", firstWeldSkips(receipt.Skipped))
	}
	return nil
}

// firstWeldSkips 把前几条跳过的行收成「序号:原因」，告警里好看。
func firstWeldSkips(skips []weldSummarySkip) []string {
	// 只取前几条，免得一条告警刷满屏。
	skips = skips[:min(len(skips), weldSkipsLogged)]
	// 按条数预留文本。
	out := make([]string, 0, len(skips))
	// 逐条拼成「序号:原因」。
	for _, s := range skips {
		// 序号和原因用冒号连起来。
		out = append(out, strconv.Itoa(s.Index)+":"+s.Reason)
	}
	return out
}
