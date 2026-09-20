package httpapi

import (
	"net/http"
	"time"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/service"
)

// mountStats 挂厂端焊汇总上送和 WAN 管理员跨厂报表。
func (h *Handler) mountStats(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/channel/weld-summaries", h.putWeldSummaries)
	mux.HandleFunc("GET /v1/weld-reports", h.listWeldReports)
}

type weldSummaryIn struct {
	Day         string `json:"day"`         // UTC 日期 YYYY-MM-DD
	ProjectName string `json:"projectName"` // 工程名快照
	WeldKind    string `json:"weldKind"`    // 焊接模式
	RunCount    int64  `json:"runCount"`    // 该粒次数
	LengthMM    int64  `json:"lengthMm"`    // 该粒焊长毫米
	DurationSec int64  `json:"durationSec"` // 该粒时长秒
}

type weldSummariesBody struct {
	Rows []weldSummaryIn `json:"rows"` // 该厂当前全量汇总
}

// 收下该厂当前焊汇总并整表替换；请求体不得带人员或组织字段。
func (h *Handler) putWeldSummaries(w http.ResponseWriter, r *http.Request) {
	fid, err := h.factoryProof(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req weldSummariesBody
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err)
		return
	}
	items := make([]service.WeldSummaryIn, 0, len(req.Rows))
	for _, row := range req.Rows {
		items = append(items, service.WeldSummaryIn{
			Day: row.Day, ProjectName: row.ProjectName, WeldKind: row.WeldKind,
			RunCount: row.RunCount, LengthMM: row.LengthMM, DurationSec: row.DurationSec,
		})
	}
	if err := h.svc.Stats.PutFromFactory(r.Context(), fid, items); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// 跨厂焊汇总，不含人员组织。
func (h *Handler) listWeldReports(w http.ResponseWriter, r *http.Request) {
	from, err := parseRFC3339Query(r.URL.Query().Get("from"))
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	to, err := parseRFC3339Query(r.URL.Query().Get("to"))
	if err != nil {
		writeBadRequest(w, err)
		return
	}
	rows, err := h.svc.Stats.ListWeldReports(r.Context(), bearer(r), r.URL.Query().Get("factoryId"), from, to)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// parseRFC3339Query 空串表示不裁；非空须是 RFC3339。
func parseRFC3339Query(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339, raw)
	}
	if err != nil {
		return nil, domain.ErrInvalidName
	}
	u := t.UTC()
	return &u, nil
}
