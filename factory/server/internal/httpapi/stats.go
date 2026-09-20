package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

// mountStats 挂焊长时长汇聚、厂端报表和下钻。
func (h *Handler) mountStats(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/factories/{id}/pad/weld-facts", h.flushWeldFacts)
	mux.HandleFunc("GET /v1/factories/{id}/pad/weld-stats", h.myWeldStats)
	mux.HandleFunc("GET /v1/factories/{id}/weld-reports", h.listWeldReports)
	mux.HandleFunc("GET /v1/factories/{id}/weld-runs", h.listWeldRuns)
	mux.HandleFunc("POST /v1/factories/{id}/weld-reports/demo", h.seedWeldDemo)
}

type weldFactReq struct {
	ID          string       `json:"id"`                    // 产生端事实身份
	CreatorID   string       `json:"creatorId"`             // 当时登录人
	ClientID    string       `json:"clientId"`              // 当时 Client，可空
	OrgUnitID   *string      `json:"orgUnitId"`             // 发生节点；厂直属为空
	OrgPath     []pathNodeIn `json:"orgPath"`               // 发生时路径
	ProjectID   string       `json:"projectId"`             // 当时工程身份，可空
	ProjectName string       `json:"projectName"`           // 当时工程名快照
	WeldKind    string       `json:"weldKind"`              // single / multilayer / tbar / 空
	LengthMM    int64        `json:"lengthMm"`              // 本段焊长毫米
	DurationSec int64        `json:"durationSec"`           // 本段时长秒
	OccurredAt  string       `json:"occurredAt"`            // RFC3339 焊完时间
}

type pathNodeIn struct {
	ID   string `json:"id"`   // 节点稳定身份
	Name string `json:"name"` // 当时显示名
}

type weldFactsBody struct {
	Facts []weldFactReq `json:"facts"` // 本机待发，可含多人
}

// 把本机待发焊事实汇进本厂；发送人不必等于创建人。
func (h *Handler) flushWeldFacts(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req weldFactsBody
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		items := make([]service.WeldFact, 0, len(req.Facts))
		for _, row := range req.Facts {
			item, err := parseWeldFact(row)
			if err != nil {
				writeBadRequest(w, err)
				return
			}
			items = append(items, item)
		}
		out, err := svc.Stats.FlushWeldFacts(r.Context(), bearer(r), items)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// 当前登录人合计、按工程和最近焊次。
func (h *Handler) myWeldStats(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		out, err := svc.Stats.MyWeldStats(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// 厂端焊长时长报表，按选定维度归集。
func (h *Handler) listWeldReports(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		from, err := parseTimeQuery(r.URL.Query().Get("from"))
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		to, err := parseTimeQuery(r.URL.Query().Get("to"))
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		group := r.URL.Query().Get("group")
		rows, err := svc.Stats.ListWeldReport(r.Context(), bearer(r), group, from, to)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
}

// 厂端每次起停下钻。
func (h *Handler) listWeldRuns(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		q, err := parseWeldRunQuery(r)
		if err != nil {
			writeBadRequest(w, err)
			return
		}
		rows, err := svc.Stats.ListWeldRuns(r.Context(), bearer(r), q)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
}

// 超管写入演示焊次，已有则跳过。
func (h *Handler) seedWeldDemo(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		out, err := svc.Stats.SeedWeldDemo(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// parseWeldFact 把 JSON 收成焊事实；身份非法当场拒绝整批。
func parseWeldFact(row weldFactReq) (service.WeldFact, error) {
	id, err := uuid.Parse(row.ID)
	if err != nil {
		return service.WeldFact{}, errInvalidID
	}
	creator, err := uuid.Parse(row.CreatorID)
	if err != nil {
		return service.WeldFact{}, errInvalidID
	}
	item := service.WeldFact{
		ID:          id,
		CreatorID:   creator,
		ProjectName: row.ProjectName,
		WeldKind:    row.WeldKind,
		LengthMM:    row.LengthMM,
		DurationSec: row.DurationSec,
		OrgPath:     make([]service.PathNode, 0, len(row.OrgPath)),
	}
	if row.ClientID != "" {
		cid, err := uuid.Parse(row.ClientID)
		if err != nil {
			return service.WeldFact{}, errInvalidID
		}
		item.ClientID = &cid
	}
	if row.OrgUnitID != nil && *row.OrgUnitID != "" {
		uid, err := uuid.Parse(*row.OrgUnitID)
		if err != nil {
			return service.WeldFact{}, errInvalidID
		}
		item.OrgUnitID = &uid
	}
	if row.ProjectID != "" {
		pid, err := uuid.Parse(row.ProjectID)
		if err != nil {
			return service.WeldFact{}, errInvalidID
		}
		item.ProjectID = &pid
	}
	for _, n := range row.OrgPath {
		nid, err := uuid.Parse(n.ID)
		if err != nil {
			return service.WeldFact{}, errInvalidID
		}
		item.OrgPath = append(item.OrgPath, service.PathNode{ID: nid, Name: n.Name})
	}
	if row.OccurredAt != "" {
		t, err := time.Parse(time.RFC3339, row.OccurredAt)
		if err != nil {
			t, err = time.Parse(time.RFC3339Nano, row.OccurredAt)
			if err != nil {
				return service.WeldFact{}, err
			}
		}
		item.OccurredAt = t
	}
	return item, nil
}

// parseWeldRunQuery 读下钻查询窗。
func parseWeldRunQuery(r *http.Request) (service.WeldRunQuery, error) {
	from, err := parseTimeQuery(r.URL.Query().Get("from"))
	if err != nil {
		return service.WeldRunQuery{}, err
	}
	to, err := parseTimeQuery(r.URL.Query().Get("to"))
	if err != nil {
		return service.WeldRunQuery{}, err
	}
	q := service.WeldRunQuery{From: from, To: to}
	person, err := parseOptUUIDQuery(r.URL.Query().Get("personId"))
	if err != nil {
		return service.WeldRunQuery{}, err
	}
	q.PersonID = person
	projRaw := r.URL.Query().Get("projectId")
	if projRaw == "none" {
		q.NoProject = true
	} else {
		proj, err := parseOptUUIDQuery(projRaw)
		if err != nil {
			return service.WeldRunQuery{}, err
		}
		q.ProjectID = proj
	}
	org, err := parseOptUUIDQuery(r.URL.Query().Get("orgUnitId"))
	if err != nil {
		return service.WeldRunQuery{}, err
	}
	q.OrgUnitID = org
	if dayRaw := r.URL.Query().Get("day"); dayRaw != "" {
		t, err := time.Parse("2006-01-02", dayRaw)
		if err != nil {
			return service.WeldRunQuery{}, err
		}
		q.Day = &t
	}
	return q, nil
}

// parseOptUUIDQuery 空表示不裁。
func parseOptUUIDQuery(raw string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, errInvalidID
	}
	return &id, nil
}

// parseTimeQuery 空表示不裁时间窗。
func parseTimeQuery(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
