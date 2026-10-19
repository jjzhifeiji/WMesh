package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

const padRecentLimit = 100 // 示教器最近焊次条数上限

const (
	weldRunsDefaultLimit = 200  // 下钻不带条数时每页默认条数
	weldRunsMaxLimit     = 1000 // 下钻每页最多条数，免得一次拖满库
)

// weldDemoNS 演示焊次的稳定命名空间，同一厂重复写入仍是原身份。
var weldDemoNS = uuid.MustParse("7c2e1a90-6b4d-4f11-9c3a-0b8e5d2f1748")

const weldDemoCount = 45 // 一次写入的演示焊次数

const (
	wanPushRetryMin    = 5 * time.Second // 焊汇总上送失败后第一次等多久再推
	wanPushRetryMax    = 5 * time.Minute // 焊汇总上送退避封顶，WAN 久断也不拖太久
	wanPushSignRetries = 2               // 签名被 WAN 拒时每批最多补推几次：回包已校正时差，厂钥真不对也不一直打
)

// errWeldLocalRead 标记读本厂焊汇总失败，后台上送据此退避重试。
var errWeldLocalRead = errors.New("read local weld summaries")

// WeldWANPoster 把无人员组织的焊汇总交给 WAN；测试可空。
// 暂时送不出（连不上、5xx、读不到钥）交回 ErrWANUnreachable，后台会退避重试；签名被拒交回 ErrWANSignatureRejected，每批限次重试；其它错误不重试。
type WeldWANPoster interface {
	// 把不含人员组织的焊汇总交给广域侧
	PostWeldSummaries(ctx context.Context, factoryID uuid.UUID, rows []WeldWANSummary) error
}

// Stats 管焊长时长汇聚与厂端报表。
type Stats struct {
	*kernel                 // 共用本厂库、审计和在线计数
	demoEnabled atomic.Bool // 只开发版打开：放开写入演示焊次；默认关，生产一条都不写
}

// SetDemoData 开发版放开写入演示焊次；生产保持关，关着时入口当作不存在。
func (s *Stats) SetDemoData(on bool) {
	// 原子记下开关，请求并发读时不会读到半截
	s.demoEnabled.Store(on)
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
	Day         string     `json:"day,omitempty"`         // 报表时区日期 YYYY-MM-DD
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
	From       *time.Time // 发生时间起，含
	To         *time.Time // 发生时间止，不含
	PersonID   *uuid.UUID // 当时登录人
	ProjectID  *uuid.UUID // 工程身份
	NoProject  bool       // 只取未打开工程
	OrgUnitID  *uuid.UUID // 发生节点
	DirectOnly bool       // 只取厂直属
	Day        *time.Time // UTC 日历日
	Limit      int        // 每页条数；0 取默认，超上限按上限
	Offset     int        // 跳过前几条
}

// WeldRunPage 是下钻一页明细，带同条件下的总条数和合计。
type WeldRunPage struct {
	Items       []WeldRunRow `json:"items"`       // 本页起停，新的在前
	Total       int64        `json:"total"`       // 同条件总条数
	LengthMM    int64        `json:"lengthMm"`    // 同条件焊长合计毫米，已封顶
	DurationSec int64        `json:"durationSec"` // 同条件时长合计秒，已封顶
}

// AuthorizeWeldFlush 汇聚前核对会话；不通过就留汇聚拒绝审计。
// HTTP 读大正文前先调它，匿名被挡时审计口径不丢。
func (s *Stats) AuthorizeWeldFlush(ctx context.Context, token string) (Account, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		// 记下汇聚焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "flush_weld_facts", "batch", audit.Deny)
		return Account{}, err
	}
	return acc, nil
}

// FlushWeldFacts 把本机待发写入厂库；发送人不必等于创建人。
func (s *Stats) FlushWeldFacts(ctx context.Context, token string, items []WeldFact) (WeldFlushResult, error) {
	// 先确认会话仍有效，不通过已留汇聚拒绝审计
	acc, err := s.AuthorizeWeldFlush(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
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
		// 焊长或时长为负、超出单条上限，或作业类型不认识，拒绝这条
		if !store.ValidWeldAmounts(item.LengthMM, item.DurationSec) || !store.ValidWeldKind(item.WeldKind) {
			// 取出失败原因放进拒绝行，不含口令
			out.Rejected = append(out.Rejected, WeldReject{ID: item.ID, Error: domain.ErrForbidden.Error()})
			continue
		}
		// 工程名去首尾空白后仍超长，拒绝这条，免得 WAN 整批拒收
		if !store.ValidWeldProjectName(item.ProjectName) {
			// 取出失败原因放进拒绝行，本机留着等改名
			out.Rejected = append(out.Rejected, WeldReject{ID: item.ID, Error: domain.ErrInvalidName.Error()})
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
	// 有新收下的焊次，厂库汇总就变了
	if len(out.Accepted) > 0 {
		// 只发信号由后台串行上送，平板不必等 WAN 回包
		s.RequestWANPush()
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

// ListWeldReport 按维度归集调用方能看的事实；按日维在 loc 时区切日，空按 UTC。
func (s *Stats) ListWeldReport(ctx context.Context, token, group string, from, to *time.Time, loc *time.Location) ([]WeldReportRow, error) {
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
	// 每个请求只读一次角色，算出能看的范围
	scope, err := s.weldScope(ctx, acc)
	// 角色或子树读失败就拒绝，不当成看不到
	if err != nil {
		// 记下查看焊报表被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "list_weld_report", "report", audit.Deny)
		return nil, err
	}
	// 在库里按维度归集，范围拼进条件，日期按调用方时区切
	agg, err := s.store.ListWeldReport(ctx, store.WeldFactFilter{From: from, To: to, Scope: scope}, group, loc)
	// 汇总查询失败则报表中止
	if err != nil {
		// 记下查看焊报表被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "list_weld_report", "report", audit.Deny)
		return nil, err
	}
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
			// 按切日时区收成日期文本，不再转回 UTC
			item.Day = row.Day.Format("2006-01-02")
		}
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, item)
	}
	// 查看焊报表成功后记审计，再把结果交回
	return out, s.audit(ctx, &acc.ID, nil, "list_weld_report", group, audit.Allow)
}

// ListWeldRuns 分页列出调用方能看的每次起停，带同条件的总条数和合计。
func (s *Stats) ListWeldRuns(ctx context.Context, token string, q WeldRunQuery) (WeldRunPage, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		// 记下查看每次起停被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "list_weld_runs", "runs", audit.Deny)
		return WeldRunPage{}, err
	}
	// 每个请求只读一次角色，算出能看的范围
	scope, err := s.weldScope(ctx, acc)
	// 角色或子树读失败就拒绝，不当成看不到
	if err != nil {
		// 记下查看每次起停被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "list_weld_runs", "runs", audit.Deny)
		return WeldRunPage{}, err
	}
	// 下钻条件和可见范围收成同一个查询窗，明细和合计共用
	filter := store.WeldFactFilter{
		From: q.From, To: q.To, CreatorID: q.PersonID, ProjectID: q.ProjectID,
		NoProject: q.NoProject, OrgUnitID: q.OrgUnitID, DirectOnly: q.DirectOnly, Day: q.Day,
		Scope: scope,
	}
	// 合计不受分页影响，按同一窗在库里算
	tot, err := s.store.WeldFactTotals(ctx, filter)
	// 合计读失败则下钻中止
	if err != nil {
		// 记下查看每次起停被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "list_weld_runs", "runs", audit.Deny)
		return WeldRunPage{}, err
	}
	// 条数按默认和上限收住，再按页取明细
	filter.Limit, filter.Offset = weldRunsPage(q.Limit, q.Offset)
	// 按页取出明细，新的在前
	facts, err := s.store.ListWeldFacts(ctx, filter)
	// 明细读失败则下钻中止
	if err != nil {
		// 记下查看每次起停被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "list_weld_runs", "runs", audit.Deny)
		return WeldRunPage{}, err
	}
	// 先备好合计，本页明细后面逐条放
	out := WeldRunPage{
		Items: make([]WeldRunRow, 0, len(facts)),
		Total: tot.RunCount, LengthMM: tot.LengthMM, DurationSec: tot.DurationSec,
	}
	// 逐条收成下钻行，范围已在库里裁过
	for _, f := range facts {
		// 把这一条收进结果，漏了清单就不齐
		out.Items = append(out.Items, WeldRunRow{
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

// SeedWeldDemo 开发版开关打开时，超管写入固定身份的演示焊次，已有则跳过；开关关着回找不到。
func (s *Stats) SeedWeldDemo(ctx context.Context, token string) (WeldDemoResult, error) {
	// 生产没开演示数据就当没有这个入口，假焊次一条都不写
	if !s.demoEnabled.Load() {
		// 记下写入演示焊次被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "seed_weld_demo", "demo", audit.Deny)
		return WeldDemoResult{}, domain.ErrNotFound
	}
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
		// 演示数据已在，也请后台补送一次，不在这里等 WAN
		s.RequestWANPush()
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
	// 演示焊次已落库，发信号由后台串行上送
	s.RequestWANPush()
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

// weldScope 只读一次角色算焊事实可见范围；空指针表示全厂，否则本人加可见子树。
// 只有超管和组织管理员、负责人、审计员能放宽，和只读查看同一套角色。
func (s *kernel) weldScope(ctx context.Context, acc Account) (*store.WeldScope, error) {
	// 只取当前有效角色，收回的不参与
	grants, err := s.grantsOf(ctx, acc.ID)
	// 角色读失败就无法判定，拒绝放行
	if err != nil {
		return nil, err
	}
	// 持有厂级超管则盖住全厂，不必再查子树
	if isFactorySA(grants) {
		return nil, nil
	}
	// 收集节点作用域的根，后面一次求出子树
	roots := make([]uuid.UUID, 0, len(grants))
	// 只看三种只读管理角色，操作员等不放宽
	for _, g := range withRoles(grants, RoleOrgAdmin, RoleOrgLead, RoleAuditor) {
		// 整厂作用域盖住全部节点和厂直属
		if g.ScopeKind == ScopeFactory {
			return nil, nil
		}
		// 节点作用域记下根节点
		if g.OrgUnitID != nil {
			// 收进根列表，子树一起求
			roots = append(roots, *g.OrgUnitID)
		}
	}
	// 先备好只看本人的范围，有子树再补上
	scope := &store.WeldScope{SelfID: acc.ID, OrgUnitIDs: []uuid.UUID{}}
	// 没有节点作用域就只看本人
	if len(roots) == 0 {
		return scope, nil
	}
	// 递归求出这些根的当前子树，不按状态裁
	units, err := s.store.OrgSubtreeIDs(ctx, roots)
	// 子树读失败则拒绝，不猜是否覆盖
	if err != nil {
		return nil, err
	}
	// 可见节点就是这些子树
	scope.OrgUnitIDs = units
	return scope, nil
}

// weldRunsPage 收住下钻分页参数：条数不给按默认，超上限按上限，负跳过当零。
func weldRunsPage(limit, offset int) (int, int) {
	// 没给条数就按默认一页
	if limit <= 0 {
		// 取默认条数
		limit = weldRunsDefaultLimit
	}
	// 超过上限就按上限，免得一次拖满库
	if limit > weldRunsMaxLimit {
		// 收到上限
		limit = weldRunsMaxLimit
	}
	// 跳过条数为负就从头取
	if offset < 0 {
		// 从第一条开始
		offset = 0
	}
	return limit, offset
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

// RequestWANPush 发「焊汇总已变」信号；已有没取走的就合并，调用方不阻塞。
func (s *Stats) RequestWANPush() {
	// 信号格空着才放，满了说明后台还会再推一次
	select {
	// 放进信号，后台循环醒来重读库再推
	case s.weldDirty <- struct{}{}:
	// 已有信号在等，这次并进去不再重复
	default:
	}
}

// SetWANPushBackoff 调后台上送的退避起点和封顶；测试调小，须在起循环前设。
func (s *Stats) SetWANPushBackoff(first, ceiling time.Duration) {
	// 记下第一次失败后等多久
	s.wanRetryMin = first
	// 记下退避最多等多久
	s.wanRetryMax = ceiling
}

// RunWANPush 后台串行上送焊汇总：一次只推一批，推送中来的信号合并成下一批；ctx 取消就退出。
func (s *Stats) RunWANPush(ctx context.Context) {
	// 一直等信号，直到这家厂关库或进程退出
	for {
		// 等「已变」信号或退出，没有变化就不推
		select {
		// 取消了就退出，关库方在等这里
		case <-ctx.Done():
			return
		// 汇总变了，往下推一批
		case <-s.weldDirty:
		}
		// 每批从退避起点开始，连续失败才加倍
		wait := s.wanRetryMin
		// 这一批还能因签名被拒补推几次，刚起进程时钟还没校正会撞上
		signLeft := wanPushSignRetries
		// 推这一批，可恢复的失败退避后重读再推
		for {
			// 重读本厂库并上送全量汇总
			err := s.PushWANSummaries(ctx)
			// 推送途中已取消就退出，不再重试
			if ctx.Err() != nil {
				return
			}
			// 推成功了就回去等下一个信号
			if err == nil {
				break
			}
			// 先按错误种类看值不值得重试
			retry := weldPushRetryable(err)
			// 签名被拒多半是钟偏，被拒的回包已按 Date 校正时差，还有名额就再签再推
			if !retry && errors.Is(err, domain.ErrWANSignatureRejected) && signLeft > 0 {
				// 用掉一次补推名额，用完再被拒就当业务拒绝
				signLeft--
				// 这次按可恢复处理，退避后重推
				retry = true
			}
			// 业务拒绝、没有钥或厂已注销，重试也没用
			if !retry {
				// 只记一次告警，等下一个信号再推
				slog.Warn("push weld summaries rejected", "factory", s.store.FactoryID(), "err", err)
				break
			}
			// 记下这次没推上，退避后再推
			slog.Warn("push weld summaries, will retry", "factory", s.store.FactoryID(), "wait", wait, "err", err)
			// 等到退避结束或取消
			select {
			// 取消了就退出，不再重试
			case <-ctx.Done():
				return
			// 退避到点再推，免得断网时打满
			case <-time.After(wait):
			}
			// 下次失败等得更久，但不超过封顶
			wait = min(wait*2, s.wanRetryMax)
			// 重推会重读库，攒下的信号已被这次盖住
			select {
			// 收掉攒下的信号，免得推完又空推一遍
			case <-s.weldDirty:
			// 没有攒下的信号就直接重推
			default:
			}
		}
	}
}

// weldPushRetryable 只有连不上 WAN 和读本厂库失败才值得退避重试。
func weldPushRetryable(err error) bool {
	// 网络失败、5xx 和本厂库读失败可恢复，其它都是拒绝
	return errors.Is(err, domain.ErrWANUnreachable) || errors.Is(err, errWeldLocalRead)
}

// PushWANSummaries 把本厂焊事实收成无人员组织的汇总交给 WAN；工程名按 WAN 口径规范，同键合并封顶。
func (s *Stats) PushWANSummaries(ctx context.Context) error {
	// 没有上送或下行通道则只在厂内记账
	if s.wanWeld == nil {
		return nil
	}
	// 取出待上送的无人员汇总
	rows, err := s.store.ListWeldWANSummaries(ctx)
	// 汇总读失败则不上送残缺，标成读本厂库失败好让后台重试
	if err != nil {
		// 套上本厂读失败的标记再交回，原错误仍留在链里。
		return fmt.Errorf("%w: %w", errWeldLocalRead, err)
	}
	// 按条数预留，规范名字后同键合并只会更少
	out := make([]WeldWANSummary, 0, len(rows))
	// 记下日、工程名、模式在结果里的位置，同键并到一行
	at := make(map[[3]string]int, len(rows))
	// 逐行规范工程名，同键的行合并，免得 WAN 撞键拒收
	for _, r := range rows {
		// 按 WAN 口径规范工程名，只差首尾空白的并成一个
		name := wanProjectName(r.ProjectName)
		// 日、工程名、模式三者一样才算同一粒
		key := [3]string{r.Day, name, r.WeldKind}
		// 这一粒已经有了就把合计加上去
		if i, ok := at[key]; ok {
			// 次数饱和相加，超过 int64 就封顶
			out[i].RunCount = satAdd(out[i].RunCount, r.RunCount)
			// 焊长饱和相加，超过 int64 就封顶
			out[i].LengthMM = satAdd(out[i].LengthMM, r.LengthMM)
			// 时长饱和相加，超过 int64 就封顶
			out[i].DurationSec = satAdd(out[i].DurationSec, r.DurationSec)
			continue
		}
		// 新的一粒，记下它在结果里的位置
		at[key] = len(out)
		// 把这一条收进结果，保持查询给的先后顺序
		out = append(out, WeldWANSummary{
			Day: r.Day, ProjectName: name, WeldKind: r.WeldKind,
			RunCount: r.RunCount, LengthMM: r.LengthMM, DurationSec: r.DurationSec,
		})
	}
	// 取出本厂稳定身份，封包和审计都要用，再交回调用方
	return s.wanWeld.PostWeldSummaries(ctx, s.store.FactoryID(), out)
}

// wanProjectName 按 WAN 口径规范工程名：去首尾空白，空归未关联，截到上限再去空白。
func wanProjectName(name string) string {
	// 先去首尾空白，制表符和全角空格一并去掉，和 WAN 一致
	name = strings.TrimSpace(name)
	// 去完是空的就归到未关联工程，不送空名
	if name == "" {
		return store.WeldUnsetProject
	}
	// 按字符拆开，截断时不切坏多字节字
	runes := []rune(name)
	// 超过上限就截到上限，WAN 不收更长的名字
	if len(runes) > store.WeldMaxProjectName {
		// 只留前面上限个字
		name = string(runes[:store.WeldMaxProjectName])
	}
	// 截断处可能正好是空白，再去一次
	return strings.TrimSpace(name)
}

// satAdd 饱和加法：越过 int64 上下限就封在边上，不回绕。
func satAdd(a, b int64) int64 {
	// 正数相加越过上限就封到上限
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	// 负数相加越过下限就封到下限
	if b < 0 && a < math.MinInt64-b {
		return math.MinInt64
	}
	// 没越界就照常相加
	return a + b
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
