package wanchannel

import (
	"context"

	"github.com/google/uuid"
)

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

// PostWeldSummaries 用厂钥把当前焊汇总 POST 给 WAN。
func PostWeldSummaries(ctx context.Context, wanHTTP string, factoryID uuid.UUID, priv []byte, rows []WeldSummaryRow) error {
	// 用厂钥签这次上送，签不出平台就不会收。
	p := newPuller(wanHTTP, factoryID, priv)
	// 没有记录也要送空表，避免对端把缺省当成没送。
	if rows == nil {
		// 空表让平台清掉旧汇总，而不是保持上一份。
		rows = []WeldSummaryRow{}
	}
	// 签完再送出，平台拒绝则整次上送失败。
	return p.postJSON(ctx, "/v1/channel/weld-summaries", weldSummariesBody{Rows: rows}, nil)
}
