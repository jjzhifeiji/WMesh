package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

const padRecentLimit = 100 // 示教器最近焊次条数上限

// weldDemoNS 演示焊次的稳定命名空间，同一厂重复写入仍是原身份。
var weldDemoNS = uuid.MustParse("7c2e1a90-6b4d-4f11-9c3a-0b8e5d2f1748")

const weldDemoCount = 45 // 一次写入的演示焊次数

// WeldWANPoster 把无人员组织的焊汇总交给 WAN；测试可空。
type WeldWANPoster interface {
	PostWeldSummaries(ctx context.Context, factoryID uuid.UUID, rows []WeldWANSummary) error
}

// Stats 管焊长时长汇聚与厂端报表。
type Stats struct{ *kernel }

// WeldWANSummary 是上送 WAN 的一粒，不含人员组织。
type WeldWANSummary struct {
	Day         string `json:"day"`         // UTC 日期 YYYY-MM-DD
	ProjectName string `json:"projectName"` // 工程名快照或未关联
	WeldKind    string `json:"weldKind"`    // 焊接模式
	RunCount    int64  `json:"runCount"`    // 该粒次数
	LengthMM    int64  `json:"lengthMm"`    // 该粒焊长毫米
	DurationSec int64  `json:"durationSec"` // 该粒时长秒
}

// PadWorkSnap 是登录时钉死的工作上下文，供本机写焊事实。
type PadWorkSnap struct {
	Direct    bool       `json:"direct"`              // 厂直属
	OrgUnitID *uuid.UUID `json:"orgUnitId,omitempty"` // 当时分配节点
	OrgPath   []PathNode `json:"orgPath"`             // 当时路径快照
}

// WeldFlushResult 是本机待发汇聚结果；拒绝的条仍留本机。
type WeldFlushResult struct {
	Accepted []uuid.UUID  `json:"accepted"` // 已写入或幂等命中
	Rejected []WeldReject `json:"rejected"` // 未写入，本机应保留
}

// WeldReject 是单条汇聚失败原因。
type WeldReject struct {
	ID    uuid.UUID `json:"id"`    // 事实身份
	Error string    `json:"error"` // 英文业务错误
}

// PadProjectStat 是当前人按工程的合计。
type PadProjectStat struct {
	ProjectID   *uuid.UUID `json:"projectId,omitempty"` // 工程身份
	ProjectName string     `json:"projectName"`         // 工程名或未关联
	RunCount    int64      `json:"runCount"`            // 次数
	LengthMM    int64      `json:"lengthMm"`            // 焊长毫米
	DurationSec int64      `json:"durationSec"`         // 时长秒
}

// PadWeldRun 是当前人的一次起停。
type PadWeldRun struct {
	ID          uuid.UUID  `json:"id"`                  // 事实身份
	ProjectID   *uuid.UUID `json:"projectId,omitempty"` // 工程身份
	ProjectName string     `json:"projectName"`         // 工程名快照
	WeldKind    string     `json:"weldKind"`            // 焊接模式
	LengthMM    int64      `json:"lengthMm"`            // 本段焊长毫米
	DurationSec int64      `json:"durationSec"`         // 本段时长秒
	OccurredAt  string     `json:"occurredAt"`          // RFC3339 焊完时间
}

// PadWeldStats 是当前登录人合计、按工程和最近焊次。
type PadWeldStats struct {
	LengthMM    int64            `json:"lengthMm"`    // 已汇聚焊长毫米
	DurationSec int64            `json:"durationSec"` // 已汇聚时长秒
	RunCount    int64            `json:"runCount"`    // 已汇聚次数
	Work        PadWorkSnap      `json:"work"`        // 当前分配快照，给新事实用
	Projects    []PadProjectStat `json:"projects"`    // 按工程合计
	Recent      []PadWeldRun     `json:"recent"`      // 最近焊次，新的在前
}

// WeldReportRow 是厂端报表一行。
type WeldReportRow struct {
	PersonID    *uuid.UUID `json:"personId,omitempty"`    // 当时登录人
	LoginName   string     `json:"loginName,omitempty"`   // 登录名
	DisplayName string     `json:"displayName,omitempty"` // 显示名
	OrgUnitID   *uuid.UUID `json:"orgUnitId,omitempty"`   // 发生节点
	OrgPath     []PathNode `json:"orgPath,omitempty"`     // 发生时路径
	Day         string     `json:"day,omitempty"`         // UTC 日期 YYYY-MM-DD
	ProjectID   *uuid.UUID `json:"projectId,omitempty"`   // 工程身份
	ProjectName string     `json:"projectName,omitempty"` // 工程名
	LengthMM    int64      `json:"lengthMm"`              // 该组焊长毫米
	DurationSec int64      `json:"durationSec"`           // 该组时长秒
	RunCount    int64      `json:"runCount"`              // 该组次数
}

// WeldRunRow 是一次起停下钻行。
type WeldRunRow struct {
	ID          uuid.UUID  `json:"id"`                  // 事实身份
	PersonID    uuid.UUID  `json:"personId"`            // 当时登录人
	LoginName   string     `json:"loginName"`           // 登录名
	DisplayName string     `json:"displayName"`         // 显示名
	OrgUnitID   *uuid.UUID `json:"orgUnitId,omitempty"` // 发生节点
	OrgPath     []PathNode `json:"orgPath"`             // 发生时路径
	ProjectID   *uuid.UUID `json:"projectId,omitempty"` // 工程身份
	ProjectName string     `json:"projectName"`         // 工程名快照
	WeldKind    string     `json:"weldKind"`            // 焊接模式
	LengthMM    int64      `json:"lengthMm"`            // 本段焊长毫米
	DurationSec int64      `json:"durationSec"`         // 本段时长秒
	OccurredAt  string     `json:"occurredAt"`          // RFC3339 焊完时间
}

// WeldDemoResult 是演示焊次写入结果。
type WeldDemoResult struct {
	Created int `json:"created"` // 本轮新写入条数
	Total   int `json:"total"`   // 本厂演示焊次总数
}

// WeldRunQuery 是下钻每次起停的查询窗。
type WeldRunQuery struct {
	From      *time.Time // 发生时间起，含
	To        *time.Time // 发生时间止，不含
	PersonID  *uuid.UUID // 当时登录人
	ProjectID *uuid.UUID // 工程身份
	NoProject bool       // 只取未打开工程
	OrgUnitID *uuid.UUID // 发生节点
	Day       *time.Time // UTC 日历日
}

// FlushWeldFacts 把本机待发写入厂库；发送人不必等于创建人。
func (s *Stats) FlushWeldFacts(ctx context.Context, token string, items []WeldFact) (WeldFlushResult, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "flush_weld_facts", "batch", audit.Deny)
		return WeldFlushResult{}, err
	}
	out := WeldFlushResult{Accepted: []uuid.UUID{}, Rejected: []WeldReject{}}
	for _, item := range items {
		if item.ID == uuid.Nil || item.CreatorID == uuid.Nil {
			out.Rejected = append(out.Rejected, WeldReject{ID: item.ID, Error: domain.ErrNotFound.Error()})
			continue
		}
		if item.LengthMM < 0 || item.DurationSec < 0 || !store.ValidWeldKind(item.WeldKind) {
			out.Rejected = append(out.Rejected, WeldReject{ID: item.ID, Error: domain.ErrForbidden.Error()})
			continue
		}
		if item.LengthMM == 0 && item.DurationSec == 0 {
			out.Rejected = append(out.Rejected, WeldReject{ID: item.ID, Error: domain.ErrForbidden.Error()})
			continue
		}
		// 创建人必须是本厂人员；路径和工程按本机快照落，不按现分配改写。
		if _, err := s.store.MergeWeldFact(ctx, item); err != nil {
			out.Rejected = append(out.Rejected, WeldReject{ID: item.ID, Error: err.Error()})
			continue
		}
		out.Accepted = append(out.Accepted, item.ID)
	}
	if len(out.Accepted) > 0 {
		// 厂库已落后再上送无人员组织的汇总；WAN 不通不影响本厂。
		if err := s.PushWANSummaries(ctx); err != nil {
			slog.Warn("push weld summaries", "err", err)
		}
	}
	return out, s.audit(ctx, &acc.ID, nil, "flush_weld_facts", "batch", audit.Allow)
}

// MyWeldStats 回当前登录人合计、按工程和最近焊次。
func (s *Stats) MyWeldStats(ctx context.Context, token string) (PadWeldStats, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "my_weld_stats", "self", audit.Deny)
		return PadWeldStats{}, err
	}
	tot, err := s.store.WeldTotalsByCreator(ctx, acc.ID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "my_weld_stats", acc.ID.String(), audit.Deny)
		return PadWeldStats{}, err
	}
	projects, err := s.store.ListWeldProjectsByCreator(ctx, acc.ID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "my_weld_stats", acc.ID.String(), audit.Deny)
		return PadWeldStats{}, err
	}
	who := acc.ID
	recent, err := s.store.ListWeldFacts(ctx, store.WeldFactFilter{CreatorID: &who, Limit: padRecentLimit})
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "my_weld_stats", acc.ID.String(), audit.Deny)
		return PadWeldStats{}, err
	}
	work, err := s.personWorkSnap(ctx, acc.ID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "my_weld_stats", acc.ID.String(), audit.Deny)
		return PadWeldStats{}, err
	}
	out := PadWeldStats{
		LengthMM: tot.LengthMM, DurationSec: tot.DurationSec, RunCount: tot.RunCount, Work: work,
		Projects: make([]PadProjectStat, 0, len(projects)),
		Recent:   make([]PadWeldRun, 0, len(recent)),
	}
	for _, p := range projects {
		out.Projects = append(out.Projects, PadProjectStat{
			ProjectID: p.ProjectID, ProjectName: p.ProjectName,
			RunCount: p.RunCount, LengthMM: p.LengthMM, DurationSec: p.DurationSec,
		})
	}
	for _, r := range recent {
		out.Recent = append(out.Recent, PadWeldRun{
			ID: r.ID, ProjectID: r.ProjectID, ProjectName: store.ProjectLabel(r.ProjectName),
			WeldKind: r.WeldKind, LengthMM: r.LengthMM, DurationSec: r.DurationSec,
			OccurredAt: r.OccurredAt.UTC().Format(time.RFC3339),
		})
	}
	return out, s.audit(ctx, &acc.ID, nil, "my_weld_stats", acc.ID.String(), audit.Allow)
}

// ListWeldReport 按维度归集调用方能看的事实。
func (s *Stats) ListWeldReport(ctx context.Context, token, group string, from, to *time.Time) ([]WeldReportRow, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "list_weld_report", "report", audit.Deny)
		return nil, err
	}
	if !validWeldGroup(group) {
		_ = s.audit(ctx, &acc.ID, nil, "list_weld_report", group, audit.Deny)
		return nil, domain.ErrForbidden
	}
	visible, err := s.visibleWeldFacts(ctx, acc, store.WeldFactFilter{From: from, To: to})
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "list_weld_report", "report", audit.Deny)
		return nil, err
	}
	agg := store.AggregateWeldFacts(visible, group)
	out := make([]WeldReportRow, 0, len(agg))
	for _, row := range agg {
		item := WeldReportRow{
			LoginName: row.LoginName, DisplayName: row.DisplayName,
			OrgUnitID: row.OrgUnitID, OrgPath: row.OrgPath,
			ProjectID: row.ProjectID, ProjectName: row.ProjectName,
			LengthMM: row.LengthMM, DurationSec: row.DurationSec, RunCount: row.RunCount,
		}
		if row.CreatorID != uuid.Nil {
			id := row.CreatorID
			item.PersonID = &id
		}
		if !row.Day.IsZero() {
			item.Day = row.Day.UTC().Format("2006-01-02")
		}
		out = append(out, item)
	}
	return out, s.audit(ctx, &acc.ID, nil, "list_weld_report", group, audit.Allow)
}

// ListWeldRuns 列出调用方能看的每次起停。
func (s *Stats) ListWeldRuns(ctx context.Context, token string, q WeldRunQuery) ([]WeldRunRow, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "list_weld_runs", "runs", audit.Deny)
		return nil, err
	}
	visible, err := s.visibleWeldFacts(ctx, acc, store.WeldFactFilter{
		From: q.From, To: q.To, CreatorID: q.PersonID, ProjectID: q.ProjectID,
		NoProject: q.NoProject, OrgUnitID: q.OrgUnitID, Day: q.Day,
	})
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "list_weld_runs", "runs", audit.Deny)
		return nil, err
	}
	out := make([]WeldRunRow, 0, len(visible))
	for _, f := range visible {
		out = append(out, WeldRunRow{
			ID: f.ID, PersonID: f.CreatorID, LoginName: f.LoginName, DisplayName: f.DisplayName,
			OrgUnitID: f.OrgUnitID, OrgPath: f.OrgPath, ProjectID: f.ProjectID,
			ProjectName: store.ProjectLabel(f.ProjectName), WeldKind: f.WeldKind,
			LengthMM: f.LengthMM, DurationSec: f.DurationSec,
			OccurredAt: f.OccurredAt.UTC().Format(time.RFC3339),
		})
	}
	return out, s.audit(ctx, &acc.ID, nil, "list_weld_runs", "runs", audit.Allow)
}

// SeedWeldDemo 超管写入固定身份的演示焊次，已有则跳过。
func (s *Stats) SeedWeldDemo(ctx context.Context, token string) (WeldDemoResult, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, "seed_weld_demo", "demo", audit.Deny)
		return WeldDemoResult{}, err
	}
	grants, err := s.grantsOf(ctx, acc.ID)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Deny)
		return WeldDemoResult{}, err
	}
	if !isFactorySA(grants) {
		_ = s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Deny)
		return WeldDemoResult{}, domain.ErrForbidden
	}
	people, err := s.store.ListPeople(ctx)
	if err != nil {
		return WeldDemoResult{}, err
	}
	active := make([]Person, 0, len(people))
	for _, p := range people {
		if p.Status == StatusActive {
			active = append(active, p)
		}
	}
	if len(active) == 0 {
		_ = s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Deny)
		return WeldDemoResult{}, domain.ErrNotFound
	}
	units, err := s.store.ListOrgUnits(ctx)
	if err != nil {
		return WeldDemoResult{}, err
	}
	fid := s.store.FactoryID()
	first := weldDemoID(fid, 0)
	if _, err := s.store.WeldFactByID(ctx, first); err == nil {
		if err := s.PushWANSummaries(ctx); err != nil {
			slog.Warn("push weld summaries", "err", err)
		}
		return WeldDemoResult{Created: 0, Total: weldDemoCount}, s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Allow)
	} else if !errors.Is(err, domain.ErrNotFound) {
		return WeldDemoResult{}, err
	}
	projNames := []string{"单层-舷侧分段", "多层-箱体", "T排-对接"}
	kinds := []string{store.WeldKindSingle, store.WeldKindMultilayer, store.WeldKindTBar}
	now := time.Now().UTC()
	created := 0
	for i := 0; i < weldDemoCount; i++ {
		who := active[i%len(active)]
		pi := i % len(projNames)
		pid := weldDemoProjectID(fid, pi)
		unitID, path := s.demoWork(ctx, who.ID, units)
		item := WeldFact{
			ID: weldDemoID(fid, i), CreatorID: who.ID, OrgUnitID: unitID, OrgPath: path,
			ProjectID: &pid, ProjectName: projNames[pi], WeldKind: kinds[pi],
			LengthMM: 800 + int64((i%12)*150), DurationSec: 40 + int64((i%9)*15),
			OccurredAt: now.Add(-time.Duration(i) * 11 * time.Hour),
		}
		if _, err := s.store.MergeWeldFact(ctx, item); err != nil {
			_ = s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Deny)
			return WeldDemoResult{}, err
		}
		created++
	}
	if err := s.PushWANSummaries(ctx); err != nil {
		slog.Warn("push weld summaries", "err", err)
	}
	return WeldDemoResult{Created: created, Total: weldDemoCount}, s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Allow)
}

// personWorkSnap 用当前有效分配钉路径；没有分配则厂直属。
func (s *kernel) personWorkSnap(ctx context.Context, personID uuid.UUID) (PadWorkSnap, error) {
	assigns, err := s.store.ActiveAssignments(ctx, personID)
	if err != nil {
		return PadWorkSnap{}, err
	}
	if len(assigns) == 0 {
		return PadWorkSnap{Direct: true, OrgPath: []PathNode{}}, nil
	}
	path, err := s.store.PathSnapshot(ctx, assigns[0].OrgUnitID)
	if err != nil {
		return PadWorkSnap{}, err
	}
	unit := assigns[0].OrgUnitID
	return PadWorkSnap{Direct: false, OrgUnitID: &unit, OrgPath: path}, nil
}

// canReadWeld 创建人可看自己；管理角色按发生节点作用域。
func (s *kernel) canReadWeld(ctx context.Context, acc Account, creatorID uuid.UUID, unit *uuid.UUID) error {
	if acc.ID == creatorID {
		return nil
	}
	grants, err := s.grantsOf(ctx, acc.ID)
	if err != nil {
		return err
	}
	if isFactorySA(grants) {
		return nil
	}
	if unit != nil {
		return s.can(ctx, acc, permView, unit)
	}
	// 厂直属行：只给整厂作用域的只读角色。
	for _, g := range withRoles(grants, RoleOrgAdmin, RoleOrgLead, RoleAuditor) {
		if g.ScopeKind == ScopeFactory {
			return nil
		}
	}
	return domain.ErrForbidden
}

// visibleWeldFacts 先按窗取事实，再按调用方作用域裁行。
func (s *Stats) visibleWeldFacts(ctx context.Context, acc Account, filter store.WeldFactFilter) ([]store.WeldFactView, error) {
	facts, err := s.store.ListWeldFacts(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]store.WeldFactView, 0, len(facts))
	for _, f := range facts {
		if err := s.canReadWeld(ctx, acc, f.CreatorID, f.OrgUnitID); err != nil {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

// demoWork 优先用人当时分配，否则轮询有效组织，再不行厂直属。
func (s *Stats) demoWork(ctx context.Context, personID uuid.UUID, units []OrgUnit) (*uuid.UUID, []PathNode) {
	assigns, err := s.store.ActiveAssignments(ctx, personID)
	if err == nil && len(assigns) > 0 {
		path, err := s.store.PathSnapshot(ctx, assigns[0].OrgUnitID)
		if err == nil {
			unit := assigns[0].OrgUnitID
			return &unit, path
		}
	}
	for _, u := range units {
		if u.Status != StatusActive {
			continue
		}
		path, err := s.store.PathSnapshot(ctx, u.ID)
		if err != nil {
			continue
		}
		id := u.ID
		return &id, path
	}
	return nil, []PathNode{}
}

// PushWANSummaries 把本厂焊事实收成无人员组织的汇总交给 WAN。
func (s *Stats) PushWANSummaries(ctx context.Context) error {
	if s.wanWeld == nil {
		return nil
	}
	rows, err := s.store.ListWeldWANSummaries(ctx)
	if err != nil {
		return err
	}
	out := make([]WeldWANSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, WeldWANSummary{
			Day: r.Day, ProjectName: r.ProjectName, WeldKind: r.WeldKind,
			RunCount: r.RunCount, LengthMM: r.LengthMM, DurationSec: r.DurationSec,
		})
	}
	return s.wanWeld.PostWeldSummaries(ctx, s.store.FactoryID(), out)
}

// validWeldGroup 报表维度是否认识。
func validWeldGroup(group string) bool {
	switch group {
	case "", store.WeldGroupPersonDay, store.WeldGroupPerson, store.WeldGroupProject, store.WeldGroupOrg, store.WeldGroupDay:
		return true
	default:
		return false
	}
}

// weldDemoID 本厂第 n 条演示焊次的稳定身份。
func weldDemoID(factoryID uuid.UUID, n int) uuid.UUID {
	return uuid.NewSHA1(weldDemoNS, []byte(fmt.Sprintf("%s:%d", factoryID.String(), n)))
}

// weldDemoProjectID 演示工程身份，不要求库里真有该工程。
func weldDemoProjectID(factoryID uuid.UUID, n int) uuid.UUID {
	return uuid.NewSHA1(weldDemoNS, []byte(fmt.Sprintf("%s:proj:%d", factoryID.String(), n)))
}
