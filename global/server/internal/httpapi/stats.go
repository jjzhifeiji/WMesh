package httpapi

import (
	"net/http"
	"time"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// mountStats 挂厂端焊汇总上送和 WAN 管理员跨厂报表。
func (h *Handler) mountStats(mux *http.ServeMux) {
	// 厂端上送当前焊汇总，整表替换。
	mux.HandleFunc("POST /v1/channel/weld-summaries", h.putWeldSummaries)
	// 管理员读跨厂焊汇总，不含人员。
	mux.HandleFunc("GET /v1/weld-reports", h.listWeldReports)
}

// 厂端上送的一日、一工程、一模式汇总。
type weldSummaryIn struct {
	Day         string `json:"day"`         // UTC 日期 YYYY-MM-DD
	ProjectName string `json:"projectName"` // 工程名快照
	WeldKind    string `json:"weldKind"`    // 焊接模式
	RunCount    int64  `json:"runCount"`    // 该粒次数
	LengthMM    int64  `json:"lengthMm"`    // 该粒焊长毫米
	DurationSec int64  `json:"durationSec"` // 该粒时长秒
}

// 该厂当前全量焊汇总，不得带人员。
type weldSummariesBody struct {
	Rows []weldSummaryIn `json:"rows"` // 该厂当前全量汇总
}

// 收下该厂当前焊汇总并整表替换；请求体不得带人员或组织字段。
func (h *Handler) putWeldSummaries(w http.ResponseWriter, r *http.Request) {
	// 留下厂或资产身份，后面用来认领或升档。
	fid, err := h.factoryProof(r)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 接住请求体，解析失败再拒绝。
	var req weldSummariesBody
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 按上送行数准备汇总，空表也会整表替换。
	items := make([]service.WeldSummaryIn, 0, len(req.Rows))
	// 逐行收成汇总，人员和组织字段不在这里。
	for _, row := range req.Rows {
		// 收成一行汇总，不带人员和组织。
		items = append(items, service.WeldSummaryIn{
			Day: row.Day, ProjectName: row.ProjectName, WeldKind: row.WeldKind,
			RunCount: row.RunCount, LengthMM: row.LengthMM, DurationSec: row.DurationSec,
		})
	}
	// 用这份汇总替换该厂全表，带人员则拒绝。
	if err := h.svc.Stats.PutFromFactory(r.Context(), fid, items); err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 成功无正文，调用方不要当成失败。
	w.WriteHeader(http.StatusNoContent)
}

// 跨厂焊汇总，不含人员组织。
func (h *Handler) listWeldReports(w http.ResponseWriter, r *http.Request) {
	// 解析时间边界，格式不对就拒绝查询。
	from, err := parseRFC3339Query(r.URL.Query().Get("from"))
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 解析时间边界，格式不对就拒绝查询。
	to, err := parseRFC3339Query(r.URL.Query().Get("to"))
	// 参数不合法就按坏请求拒绝。
	if err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 读取要筛选的厂，空表示跨厂。
	rows, err := h.svc.Stats.ListWeldReports(r.Context(), bearer(r), r.URL.Query().Get("factoryId"), from, to)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, rows)
}

// parseRFC3339Query 空串表示不裁；非空须是 RFC3339。
func parseRFC3339Query(raw string) (*time.Time, error) {
	// 没传时间就表示这一端不裁剪。
	if raw == "" {
		return nil, nil
	}
	// 解析时间边界，格式不对就拒绝查询。
	t, err := time.Parse(time.RFC3339Nano, raw)
	// 失败就停住，避免把半成品当成完成。
	if err != nil {
		// 解析时间边界，格式不对就拒绝查询。
		t, err = time.Parse(time.RFC3339, raw)
	}
	// 失败把原因交回去，避免留下残缺结果。
	if err != nil {
		return nil, domain.ErrInvalidName
	}
	// 收成协调世界时，避免各厂钟差。
	u := t.UTC()
	return &u, nil
}
