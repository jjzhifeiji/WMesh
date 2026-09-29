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
	// 把不含人员组织的焊汇总交给广域侧
	PostWeldSummaries(ctx context.Context, factoryID uuid.UUID, rows []WeldWANSummary) error
}

// Stats 管焊长时长汇聚与厂端报表。
type Stats struct {
	*kernel // 共用本厂库、审计和在线计数
}

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
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		// 记下汇聚焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "flush_weld_facts", "batch", audit.Deny)
		return WeldFlushResult{}, err
	}
	// 先备好收下和拒绝两栏，失败的条仍留本机
	out := WeldFlushResult{Accepted: []uuid.UUID{}, Rejected: []WeldReject{}}
	// 逐条待发或清单处理，失败的留下次
	for _, item := range items {
		// 身份不齐则拒绝这条，避免写下没有归属的记录
		if item.ID == uuid.Nil || item.CreatorID == uuid.Nil {
			// 取出失败原因放进拒绝行，不含口令
			out.Rejected = append(out.Rejected, WeldReject{ID: item.ID, Error: domain.ErrNotFound.Error()})
			continue
		}
		// 焊长或时长为负，或作业类型不认识，拒绝这条
		if item.LengthMM < 0 || item.DurationSec < 0 || !store.ValidWeldKind(item.WeldKind) {
			// 取出失败原因放进拒绝行，不含口令
			out.Rejected = append(out.Rejected, WeldReject{ID: item.ID, Error: domain.ErrForbidden.Error()})
			continue
		}
		// 焊长和时长都是零则拒绝，空跑不算一次焊
		if item.LengthMM == 0 && item.DurationSec == 0 {
			// 取出失败原因放进拒绝行，不含口令
			out.Rejected = append(out.Rejected, WeldReject{ID: item.ID, Error: domain.ErrForbidden.Error()})
			continue
		}
		// 创建人必须是本厂人员；路径和工程按本机快照落，不按现分配改写。
		if _, err := s.store.MergeWeldFact(ctx, item); err != nil {
			// 取出失败原因放进拒绝行，不含口令
			out.Rejected = append(out.Rejected, WeldReject{ID: item.ID, Error: err.Error()})
			continue
		}
		// 把这一条收进结果，漏了清单就不齐
		out.Accepted = append(out.Accepted, item.ID)
	}
	// 条数超过上限就停，避免把袋或报表撑满
	if len(out.Accepted) > 0 {
		// 厂库已落后再上送无人员组织的汇总；WAN 不通不影响本厂。
		if err := s.PushWANSummaries(ctx); err != nil {
			// 只记告警，不打断主流程
			slog.Warn("push weld summaries", "err", err)
		}
	}
	// 汇聚焊次成功后记审计，再把结果交回
	return out, s.audit(ctx, &acc.ID, nil, "flush_weld_facts", "batch", audit.Allow)
}

// MyWeldStats 回当前登录人合计、按工程和最近焊次。
func (s *Stats) MyWeldStats(ctx context.Context, token string) (PadWeldStats, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		// 记下查看本人焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "my_weld_stats", "self", audit.Deny)
		return PadWeldStats{}, err
	}
	// 按创建人合计焊长和时长
	tot, err := s.store.WeldTotalsByCreator(ctx, acc.ID)
	// 合计读失败则个人报表中止
	if err != nil {
		// 记下查看本人焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "my_weld_stats", acc.ID.String(), audit.Deny)
		return PadWeldStats{}, err
	}
	// 按创建人列出工程合计
	projects, err := s.store.ListWeldProjectsByCreator(ctx, acc.ID)
	// 工程合计读失败则个人报表中止
	if err != nil {
		// 记下查看本人焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "my_weld_stats", acc.ID.String(), audit.Deny)
		return PadWeldStats{}, err
	}
	// 另拷操作者再取址，审计指向这次的人
	who := acc.ID
	// 按时间窗取出焊事实
	recent, err := s.store.ListWeldFacts(ctx, store.WeldFactFilter{CreatorID: &who, Limit: padRecentLimit})
	// 事实读失败则报表中止
	if err != nil {
		// 记下查看本人焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "my_weld_stats", acc.ID.String(), audit.Deny)
		return PadWeldStats{}, err
	}
	// 用当前有效分配钉路径，没有则厂直属
	work, err := s.personWorkSnap(ctx, acc.ID)
	// 路径钉不住则这条焊次拒绝
	if err != nil {
		// 记下查看本人焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "my_weld_stats", acc.ID.String(), audit.Deny)
		return PadWeldStats{}, err
	}
	// 先备好本人合计、分工程和最近焊次
	out := PadWeldStats{
		LengthMM: tot.LengthMM, DurationSec: tot.DurationSec, RunCount: tot.RunCount, Work: work,
		Projects: make([]PadProjectStat, 0, len(projects)),
		Recent:   make([]PadWeldRun, 0, len(recent)),
	}
	// 逐条处理，某一条失败不把整批悄悄算成功
	for _, p := range projects {
		// 把这一条收进结果，漏了清单就不齐
		out.Projects = append(out.Projects, PadProjectStat{
			ProjectID: p.ProjectID, ProjectName: p.ProjectName,
			RunCount: p.RunCount, LengthMM: p.LengthMM, DurationSec: p.DurationSec,
		})
	}
	// 逐条处理，某一条失败不把整批悄悄算成功
	for _, r := range recent {
		// 把这一条收进结果，漏了清单就不齐
		out.Recent = append(out.Recent, PadWeldRun{
			ID: r.ID, ProjectID: r.ProjectID, ProjectName: store.ProjectLabel(r.ProjectName),
			WeldKind: r.WeldKind, LengthMM: r.LengthMM, DurationSec: r.DurationSec,
			OccurredAt: r.OccurredAt.UTC().Format(time.RFC3339),
		})
	}
	// 查看本人焊次成功后记审计，再把结果交回
	return out, s.audit(ctx, &acc.ID, nil, "my_weld_stats", acc.ID.String(), audit.Allow)
}

// ListWeldReport 按维度归集调用方能看的事实。
func (s *Stats) ListWeldReport(ctx context.Context, token, group string, from, to *time.Time) ([]WeldReportRow, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		// 记下查看焊报表被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "list_weld_report", "report", audit.Deny)
		return nil, err
	}
	// 维度不认识则拒绝这次报表
	if !validWeldGroup(group) {
		// 记下查看焊报表被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "list_weld_report", group, audit.Deny)
		return nil, domain.ErrForbidden
	}
	// 先按窗取再按作用域裁行
	visible, err := s.visibleWeldFacts(ctx, acc, store.WeldFactFilter{From: from, To: to})
	// 裁剪失败则不返回越权的焊次
	if err != nil {
		// 记下查看焊报表被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "list_weld_report", "report", audit.Deny)
		return nil, err
	}
	// 按维度归集焊长和时长
	agg := store.AggregateWeldFacts(visible, group)
	// 按条数决定是空、超限还是继续
	out := make([]WeldReportRow, 0, len(agg))
	// 逐条处理，某一条失败不把整批悄悄算成功
	for _, row := range agg {
		// 组装报表一行，看不到的人不会进这里
		item := WeldReportRow{
			LoginName: row.LoginName, DisplayName: row.DisplayName,
			OrgUnitID: row.OrgUnitID, OrgPath: row.OrgPath,
			ProjectID: row.ProjectID, ProjectName: row.ProjectName,
			LengthMM: row.LengthMM, DurationSec: row.DurationSec, RunCount: row.RunCount,
		}
		// 不是创建人则个人级拒绝，或改看授权
		if row.CreatorID != uuid.Nil {
			// 另拷一份身份再取址，避免下一轮把指针改掉
			id := row.CreatorID
			// 报表行带上创建人，没有人则留空
			item.PersonID = &id
		}
		// 没有日期的汇总行不送出
		if !row.Day.IsZero() {
			// 收成稳定时间文本
			item.Day = row.Day.UTC().Format("2006-01-02")
		}
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, item)
	}
	// 查看焊报表成功后记审计，再把结果交回
	return out, s.audit(ctx, &acc.ID, nil, "list_weld_report", group, audit.Allow)
}

// ListWeldRuns 列出调用方能看的每次起停。
func (s *Stats) ListWeldRuns(ctx context.Context, token string, q WeldRunQuery) ([]WeldRunRow, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		// 记下查看每次起停被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "list_weld_runs", "runs", audit.Deny)
		return nil, err
	}
	// 先按窗取再按作用域裁行
	visible, err := s.visibleWeldFacts(ctx, acc, store.WeldFactFilter{
		From: q.From, To: q.To, CreatorID: q.PersonID, ProjectID: q.ProjectID,
		NoProject: q.NoProject, OrgUnitID: q.OrgUnitID, Day: q.Day,
	})
	// 裁剪失败则不返回越权的焊次
	if err != nil {
		// 记下查看每次起停被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "list_weld_runs", "runs", audit.Deny)
		return nil, err
	}
	// 按条数决定是空、超限还是继续
	out := make([]WeldRunRow, 0, len(visible))
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, f := range visible {
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, WeldRunRow{
			ID: f.ID, PersonID: f.CreatorID, LoginName: f.LoginName, DisplayName: f.DisplayName,
			OrgUnitID: f.OrgUnitID, OrgPath: f.OrgPath, ProjectID: f.ProjectID,
			ProjectName: store.ProjectLabel(f.ProjectName), WeldKind: f.WeldKind,
			LengthMM: f.LengthMM, DurationSec: f.DurationSec,
			OccurredAt: f.OccurredAt.UTC().Format(time.RFC3339),
		})
	}
	// 查看每次起停成功后记审计，再把结果交回
	return out, s.audit(ctx, &acc.ID, nil, "list_weld_runs", "runs", audit.Allow)
}

// SeedWeldDemo 超管写入固定身份的演示焊次，已有则跳过。
func (s *Stats) SeedWeldDemo(ctx context.Context, token string) (WeldDemoResult, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		// 记下写入演示焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "seed_weld_demo", "demo", audit.Deny)
		return WeldDemoResult{}, err
	}
	// 只取当前有效角色，收回的不参与
	grants, err := s.grantsOf(ctx, acc.ID)
	// 角色读失败就无法判定，拒绝放行
	if err != nil {
		// 记下写入演示焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Deny)
		return WeldDemoResult{}, err
	}
	// 持有厂级超管则盖住全厂，不必再查子树
	if !isFactorySA(grants) {
		// 记下写入演示焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Deny)
		return WeldDemoResult{}, domain.ErrForbidden
	}
	// 列出本厂人员，不含口令和激活码
	people, err := s.store.ListPeople(ctx)
	// 人员列表读失败，名册不交残缺
	if err != nil {
		return WeldDemoResult{}, err
	}
	// 按条数决定是空、超限还是继续
	active := make([]Person, 0, len(people))
	// 逐个人员收进名册，并标在线和最近设备
	for _, p := range people {
		// 已经有效则不必再激活，直接交回账号
		if p.Status == StatusActive {
			// 把这一条收进结果，漏了清单就不齐
			active = append(active, p)
		}
	}
	// 没有有效分配则改走厂直属或拒绝
	if len(active) == 0 {
		// 记下写入演示焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Deny)
		return WeldDemoResult{}, domain.ErrNotFound
	}
	// 列出本厂组织节点
	units, err := s.store.ListOrgUnits(ctx)
	// 组织读失败，名册不交残缺
	if err != nil {
		return WeldDemoResult{}, err
	}
	// 取出本厂稳定身份，封包和审计都要用
	fid := s.store.FactoryID()
	// 同一厂重复写入仍是原来的身份
	first := weldDemoID(fid, 0)
	// 没有错误才采用这次结果，失败另走拒绝
	if _, err := s.store.WeldFactByID(ctx, first); err == nil {
		// 汇总或上送失败则本次不算送出
		if err := s.PushWANSummaries(ctx); err != nil {
			// 只记告警，不打断主流程
			slog.Warn("push weld summaries", "err", err)
		}
		// 写入演示焊次成功后记审计，再把结果交回
		return WeldDemoResult{Created: 0, Total: weldDemoCount}, s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Allow)
		// 不是没有记录，读取真失败必须停住
	} else if !errors.Is(err, domain.ErrNotFound) {
		return WeldDemoResult{}, err
	}
	// 演示工程名只用于报表展示，不要求库里真有
	projNames := []string{"单层-舷侧分段", "多层-箱体", "T排-对接"}
	// 演示按单层、多层、T排三种摊开
	kinds := []string{store.WeldKindSingle, store.WeldKindMultilayer, store.WeldKindTBar}
	// 统一到协调世界时再比较或签名
	now := time.Now().UTC()
	// 记下这次新写了几条，已有的不算
	created := 0
	// 按固定次数写下演示数据，已有身份则跳过
	for i := 0; i < weldDemoCount; i++ {
		// 按条数决定是空、超限还是继续
		who := active[i%len(active)]
		// 按条数决定是空、超限还是继续
		pi := i % len(projNames)
		// 演示工程身份，不要求库里真有该工程
		pid := weldDemoProjectID(fid, pi)
		// 演示优先用当时分配，否则有效组织
		unitID, path := s.demoWork(ctx, who.ID, units)
		// 组装一条演示焊次，身份稳定可重复写
		item := WeldFact{
			ID: weldDemoID(fid, i), CreatorID: who.ID, OrgUnitID: unitID, OrgPath: path,
			ProjectID: &pid, ProjectName: projNames[pi], WeldKind: kinds[pi],
			LengthMM: 800 + int64((i%12)*150), DurationSec: 40 + int64((i%9)*15),
			OccurredAt: now.Add(-time.Duration(i) * 11 * time.Hour),
		}
		// 写入失败则这一条留下次再试
		if _, err := s.store.MergeWeldFact(ctx, item); err != nil {
			// 记下写入演示焊次被拒绝，写失败不改变结果
			_ = s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Deny)
			return WeldDemoResult{}, err
		}
		// 这条是新写的，计数加一
		created++
	}
	// 汇总或上送失败则本次不算送出
	if err := s.PushWANSummaries(ctx); err != nil {
		// 只记告警，不打断主流程
		slog.Warn("push weld summaries", "err", err)
	}
	// 写入演示焊次成功后记审计，再把结果交回
	return WeldDemoResult{Created: created, Total: weldDemoCount}, s.audit(ctx, &acc.ID, nil, "seed_weld_demo", "demo", audit.Allow)
}

// personWorkSnap 用当前有效分配钉路径；没有分配则厂直属。
func (s *kernel) personWorkSnap(ctx context.Context, personID uuid.UUID) (PadWorkSnap, error) {
	// 只取仍有效的人员分配
	assigns, err := s.store.ActiveAssignments(ctx, personID)
	// 分配读失败，不能钉工作上下文
	if err != nil {
		return PadWorkSnap{}, err
	}
	// 没有有效分配则改走厂直属或拒绝
	if len(assigns) == 0 {
		return PadWorkSnap{Direct: true, OrgPath: []PathNode{}}, nil
	}
	// 冻结当时的组织路径
	path, err := s.store.PathSnapshot(ctx, assigns[0].OrgUnitID)
	// 路径冻结失败，拒绝写下残缺事实
	if err != nil {
		return PadWorkSnap{}, err
	}
	// 用当前有效分配上的节点钉路径
	unit := assigns[0].OrgUnitID
	return PadWorkSnap{Direct: false, OrgUnitID: &unit, OrgPath: path}, nil
}

// canReadWeld 创建人可看自己；管理角色按发生节点作用域。
func (s *kernel) canReadWeld(ctx context.Context, acc Account, creatorID uuid.UUID, unit *uuid.UUID) error {
	// 创建人可以看自己的元数据或焊次
	if acc.ID == creatorID {
		return nil
	}
	// 只取当前有效角色，收回的不参与
	grants, err := s.grantsOf(ctx, acc.ID)
	// 角色读失败就无法判定，拒绝放行
	if err != nil {
		return err
	}
	// 持有厂级超管则盖住全厂，不必再查子树
	if isFactorySA(grants) {
		return nil
	}
	// 分配到了节点就按该节点钉路径
	if unit != nil {
		// 按角色并集核对有没有这份许可，再交回调用方
		return s.can(ctx, acc, permView, unit)
	}
	// 厂直属行：只给整厂作用域的只读角色。
	for _, g := range withRoles(grants, RoleOrgAdmin, RoleOrgLead, RoleAuditor) {
		// 整厂作用域盖住全部节点
		if g.ScopeKind == ScopeFactory {
			return nil
		}
	}
	return domain.ErrForbidden
}

// visibleWeldFacts 先按窗取事实，再按调用方作用域裁行。
func (s *Stats) visibleWeldFacts(ctx context.Context, acc Account, filter store.WeldFactFilter) ([]store.WeldFactView, error) {
	// 按时间窗取出焊事实
	facts, err := s.store.ListWeldFacts(ctx, filter)
	// 事实读失败则报表中止
	if err != nil {
		return nil, err
	}
	// 按条数决定是空、超限还是继续
	out := make([]store.WeldFactView, 0, len(facts))
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, f := range facts {
		// 看不到这条焊次，从结果里拿掉
		if err := s.canReadWeld(ctx, acc, f.CreatorID, f.OrgUnitID); err != nil {
			continue
		}
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, f)
	}
	return out, nil
}

// demoWork 优先用人当时分配，否则轮询有效组织，再不行厂直属。
func (s *Stats) demoWork(ctx context.Context, personID uuid.UUID, units []OrgUnit) (*uuid.UUID, []PathNode) {
	// 只取仍有效的人员分配
	assigns, err := s.store.ActiveAssignments(ctx, personID)
	// 没有错误才采用这次结果，失败另走拒绝
	if err == nil && len(assigns) > 0 {
		// 冻结当时的组织路径
		path, err := s.store.PathSnapshot(ctx, assigns[0].OrgUnitID)
		// 没有错误才采用这次结果，失败另走拒绝
		if err == nil {
			// 用当前有效分配上的节点钉路径
			unit := assigns[0].OrgUnitID
			return &unit, path
		}
	}
	// 逐条处理，某一条失败不把整批悄悄算成功
	for _, u := range units {
		// 这一条不符合就跳过，其它条继续
		if u.Status != StatusActive {
			continue
		}
		// 冻结当时的组织路径
		path, err := s.store.PathSnapshot(ctx, u.ID)
		// 路径冻结失败，拒绝写下残缺事实
		if err != nil {
			continue
		}
		// 另拷一份身份再取址，避免下一轮把指针改掉
		id := u.ID
		return &id, path
	}
	return nil, []PathNode{}
}

// PushWANSummaries 把本厂焊事实收成无人员组织的汇总交给 WAN。
func (s *Stats) PushWANSummaries(ctx context.Context) error {
	// 没有上送或下行通道则只在厂内记账
	if s.wanWeld == nil {
		return nil
	}
	// 取出待上送的无人员汇总
	rows, err := s.store.ListWeldWANSummaries(ctx)
	// 汇总读失败则不上送残缺
	if err != nil {
		return err
	}
	// 按条数决定是空、超限还是继续
	out := make([]WeldWANSummary, 0, len(rows))
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, r := range rows {
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, WeldWANSummary{
			Day: r.Day, ProjectName: r.ProjectName, WeldKind: r.WeldKind,
			RunCount: r.RunCount, LengthMM: r.LengthMM, DurationSec: r.DurationSec,
		})
	}
	// 取出本厂稳定身份，封包和审计都要用，再交回调用方
	return s.wanWeld.PostWeldSummaries(ctx, s.store.FactoryID(), out)
}

// validWeldGroup 报表维度是否认识。
func validWeldGroup(group string) bool {
	// 只接受已知的报表维度
	switch group {
	// 这些维度认识，其它维度拒绝
	case "", store.WeldGroupPersonDay, store.WeldGroupPerson, store.WeldGroupProject, store.WeldGroupOrg, store.WeldGroupDay:
		return true
	// 不认识的报表维度直接拒绝
	default:
		return false
	}
}

// weldDemoID 本厂第 n 条演示焊次的稳定身份。
func weldDemoID(factoryID uuid.UUID, n int) uuid.UUID {
	// 拼审计用的短句，不含钥和扩展值，再交回调用方
	return uuid.NewSHA1(weldDemoNS, []byte(fmt.Sprintf("%s:%d", factoryID.String(), n)))
}

// weldDemoProjectID 演示工程身份，不要求库里真有该工程。
func weldDemoProjectID(factoryID uuid.UUID, n int) uuid.UUID {
	// 拼审计用的短句，不含钥和扩展值，再交回调用方
	return uuid.NewSHA1(weldDemoNS, []byte(fmt.Sprintf("%s:proj:%d", factoryID.String(), n)))
}
