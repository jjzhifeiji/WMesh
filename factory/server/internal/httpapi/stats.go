package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/service"
)

// mountStats 挂焊长时长汇聚、厂端报表和下钻。
func (h *Handler) mountStats(mux *http.ServeMux) {
	// 汇入焊事实，发送人可以不是创建人。
	mux.HandleFunc("POST /v1/factories/{id}/pad/weld-facts", h.flushWeldFacts)
	// 返回当前登录人的焊长和最近焊次。
	mux.HandleFunc("GET /v1/factories/{id}/pad/weld-stats", h.myWeldStats)
	// 按选定维度返回厂端焊长报表。
	mux.HandleFunc("GET /v1/factories/{id}/weld-reports", h.listWeldReports)
	// 按下钻条件返回每一次起停。
	mux.HandleFunc("GET /v1/factories/{id}/weld-runs", h.listWeldRuns)
	// 超管写入演示焊次，已经有了就跳过。
	mux.HandleFunc("POST /v1/factories/{id}/weld-reports/demo", h.seedWeldDemo)
}

// 一条待汇入的焊事实，身份必须合法。
type weldFactReq struct {
	ID          string       `json:"id"`          // 产生端事实身份
	CreatorID   string       `json:"creatorId"`   // 当时登录人
	ClientID    string       `json:"clientId"`    // 当时 Client，可空
	OrgUnitID   *string      `json:"orgUnitId"`   // 发生节点；厂直属为空
	OrgPath     []pathNodeIn `json:"orgPath"`     // 发生时路径
	ProjectID   string       `json:"projectId"`   // 当时工程身份，可空
	ProjectName string       `json:"projectName"` // 当时工程名快照
	WeldKind    string       `json:"weldKind"`    // single / multilayer / tbar / 空
	LengthMM    int64        `json:"lengthMm"`    // 本段焊长毫米
	DurationSec int64        `json:"durationSec"` // 本段时长秒
	OccurredAt  string       `json:"occurredAt"`  // RFC3339 焊完时间
}

// 焊事实里的一级组织，记下当时的名称。
type pathNodeIn struct {
	ID   string `json:"id"`   // 节点稳定身份
	Name string `json:"name"` // 当时显示名
}

// 一批焊事实，其中一条非法就整批拒绝。
type weldFactsBody struct {
	Facts []weldFactReq `json:"facts"` // 本机待发，可含多人
}

// 把本机待发焊事实汇进本厂；发送人不必等于创建人。
func (h *Handler) flushWeldFacts(w http.ResponseWriter, r *http.Request) {
	// 把本机待发焊事实汇进本厂；发送人不必等于创建人。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接一批焊事实，一条非法则整批拒绝。
		var req weldFactsBody
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 先留出列表，再逐条放进要返回的内容。
		items := make([]service.WeldFact, 0, len(req.Facts))
		// 逐条收成焊事实，一处非法则整批拒绝。
		for _, row := range req.Facts {
			// 收成一条焊事实，编号非法则整批拒绝。
			item, err := parseWeldFact(row)
			// 解析失败则拒绝，不用这个残缺的值继续。
			if err != nil {
				// 请求不合法，回非法请求并不进入业务。
				writeBadRequest(w, err)
				return
			}
			// 把这一项收进结果，供后面返回。
			items = append(items, item)
		}
		// 把焊事实汇进本厂，发送人不必是创建人。
		out, err := svc.Stats.FlushWeldFacts(r.Context(), bearer(r), items)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, out)
	})
}

// 当前登录人合计、按工程和最近焊次。
func (h *Handler) myWeldStats(w http.ResponseWriter, r *http.Request) {
	// 当前登录人合计、按工程和最近焊次。
	h.withFactory(w, r, func(svc *service.Service) {
		// 返回当前登录人的合计、分工程和最近焊次。
		out, err := svc.Stats.MyWeldStats(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, out)
	})
}

// 厂端焊长时长报表，按选定维度归集。
func (h *Handler) listWeldReports(w http.ResponseWriter, r *http.Request) {
	// 厂端焊长时长报表，按选定维度归集。
	h.withFactory(w, r, func(svc *service.Service) {
		// 读取查询条件，空着表示这一项不限制。
		from, err := parseTimeQuery(r.URL.Query().Get("from"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 读取查询条件，空着表示这一项不限制。
		to, err := parseTimeQuery(r.URL.Query().Get("to"))
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 读取归集维度，不认识的由业务拒绝。
		group := r.URL.Query().Get("group")
		// 按选定维度归集厂端焊长，越权则拒绝。
		rows, err := svc.Stats.ListWeldReport(r.Context(), bearer(r), group, from, to)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, rows)
	})
}

// 厂端每次起停下钻。
func (h *Handler) listWeldRuns(w http.ResponseWriter, r *http.Request) {
	// 按下钻条件返回每次起停，越权则拒绝。
	h.withFactory(w, r, func(svc *service.Service) {
		// 解析下钻条件，时间或编号非法则整段拒绝。
		q, err := parseWeldRunQuery(r)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 按下钻条件返回每次起停，越权则拒绝。
		rows, err := svc.Stats.ListWeldRuns(r.Context(), bearer(r), q)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, rows)
	})
}

// 超管写入演示焊次，已有则跳过。
func (h *Handler) seedWeldDemo(w http.ResponseWriter, r *http.Request) {
	// 超管写入演示焊次，已有则跳过。
	h.withFactory(w, r, func(svc *service.Service) {
		// 写入演示焊次，已经存在就跳过。
		out, err := svc.Stats.SeedWeldDemo(r.Context(), bearer(r))
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, out)
	})
}

// parseWeldFact 把 JSON 收成焊事实；身份非法当场拒绝整批。
func parseWeldFact(row weldFactReq) (service.WeldFact, error) {
	// 这条事实的身份必须合法，否则整批拒绝。
	id, err := uuid.Parse(row.ID)
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		return service.WeldFact{}, errInvalidID
	}
	// 创建人必须是合法身份，否则整批拒绝。
	creator, err := uuid.Parse(row.CreatorID)
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		return service.WeldFact{}, errInvalidID
	}
	// 先收必填的焊事实，可选身份后面再补。
	item := service.WeldFact{
		ID:          id,
		CreatorID:   creator,
		ProjectName: row.ProjectName,
		WeldKind:    row.WeldKind,
		LengthMM:    row.LengthMM,
		DurationSec: row.DurationSec,
		OrgPath:     make([]service.PathNode, 0, len(row.OrgPath)),
	}
	// 带了设备才解析，空着则不记设备。
	if row.ClientID != "" {
		// 设备编号必须合法，否则整批拒绝。
		cid, err := uuid.Parse(row.ClientID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			return service.WeldFact{}, errInvalidID
		}
		// 带了设备才记下，避免写入空编号。
		item.ClientID = &cid
	}
	// 带了组织才解析，空着则不记节点。
	if row.OrgUnitID != nil && *row.OrgUnitID != "" {
		// 组织编号必须合法，否则整批拒绝。
		uid, err := uuid.Parse(*row.OrgUnitID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			return service.WeldFact{}, errInvalidID
		}
		// 带了组织才记下归属节点。
		item.OrgUnitID = &uid
	}
	// 带了工程才解析，空着则不记工程。
	if row.ProjectID != "" {
		// 工程编号必须合法，否则整批拒绝。
		pid, err := uuid.Parse(row.ProjectID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			return service.WeldFact{}, errInvalidID
		}
		// 带了工程才记下当时所用的工程。
		item.ProjectID = &pid
	}
	// 逐级解析组织路径，一处非法则整批拒绝。
	for _, n := range row.OrgPath {
		// 路径上的节点必须合法，否则整批拒绝。
		nid, err := uuid.Parse(n.ID)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			return service.WeldFact{}, errInvalidID
		}
		// 把这一级组织补进路径，名称按当时快照。
		item.OrgPath = append(item.OrgPath, service.PathNode{ID: nid, Name: n.Name})
	}
	// 带了焊完时间才解析，空着则不填时刻。
	if row.OccurredAt != "" {
		// 按约定格式解析时刻，失败则拒绝。
		t, err := time.Parse(time.RFC3339, row.OccurredAt)
		// 失败则停止，不把这一步当成成功。
		if err != nil {
			// 常规格式失败后，再试带小数的时间。
			t, err = time.Parse(time.RFC3339Nano, row.OccurredAt)
			// 失败则停止，不把这一步当成成功。
			if err != nil {
				return service.WeldFact{}, err
			}
		}
		// 带了发生时间就按它入账。
		item.OccurredAt = t
	}
	return item, nil
}

// parseWeldRunQuery 读下钻查询窗。
func parseWeldRunQuery(r *http.Request) (service.WeldRunQuery, error) {
	// 读取查询条件，空着表示这一项不限制。
	from, err := parseTimeQuery(r.URL.Query().Get("from"))
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		return service.WeldRunQuery{}, err
	}
	// 读取查询条件，空着表示这一项不限制。
	to, err := parseTimeQuery(r.URL.Query().Get("to"))
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		return service.WeldRunQuery{}, err
	}
	// 先收下时间范围，其余筛选再补上。
	q := service.WeldRunQuery{From: from, To: to}
	// 读取查询条件，空着表示这一项不限制。
	person, err := parseOptUUIDQuery(r.URL.Query().Get("personId"))
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		return service.WeldRunQuery{}, err
	}
	// 按人下钻时收窄到这一个账号。
	q.PersonID = person
	// 读取工程筛选，特殊值表示只要无工程。
	projRaw := r.URL.Query().Get("projectId")
	// 指定无工程时，只看没有挂工程的焊次。
	if projRaw == "none" {
		// 只要没有挂工程的那些焊次。
		q.NoProject = true
		// 不是无工程则按编号筛选，非法则拒绝。
	} else {
		// 解析可选编号，空着表示不按它筛选。
		proj, err := parseOptUUIDQuery(projRaw)
		// 解析失败则拒绝，不用这个残缺的值继续。
		if err != nil {
			return service.WeldRunQuery{}, err
		}
		// 按指定工程收窄下钻结果。
		q.ProjectID = proj
	}
	// 读取查询条件，空着表示这一项不限制。
	org, err := parseOptUUIDQuery(r.URL.Query().Get("orgUnitId"))
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		return service.WeldRunQuery{}, err
	}
	// 按组织节点收窄下钻结果。
	q.OrgUnitID = org
	// 带了自然日才解析，格式不对则拒绝。
	if dayRaw := r.URL.Query().Get("day"); dayRaw != "" {
		// 按自然日解析，格式不对则拒绝整段查询。
		t, err := time.Parse("2006-01-02", dayRaw)
		// 失败则停止，不把这一步当成成功。
		if err != nil {
			return service.WeldRunQuery{}, err
		}
		// 按自然日再收窄，日期非法则拒绝。
		q.Day = &t
	}
	return q, nil
}

// parseOptUUIDQuery 空表示不裁。
func parseOptUUIDQuery(raw string) (*uuid.UUID, error) {
	// 没带这个筛选就不管它。
	if raw == "" {
		return nil, nil
	}
	// 能解析才当作设备，非法就当没传。
	id, err := uuid.Parse(raw)
	// 解析失败则拒绝，不用这个残缺的值继续。
	if err != nil {
		return nil, errInvalidID
	}
	return &id, nil
}

// parseTimeQuery 空表示不裁时间窗。
func parseTimeQuery(raw string) (*time.Time, error) {
	// 没带时间就表示这一头不限制。
	if raw == "" {
		return nil, nil
	}
	// 按约定格式解析时刻，失败则拒绝。
	t, err := time.Parse(time.RFC3339, raw)
	// 失败则停止，不把这一步当成成功。
	if err != nil {
		return nil, err
	}
	return &t, nil
}
