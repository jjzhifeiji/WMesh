package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/domain"
)

const (
	WeldKindSingle     = "single"     // 单层焊道
	WeldKindMultilayer = "multilayer" // 多层焊缝
	WeldKindTBar       = "tbar"       // T 排对接
	WeldUnsetProject   = "未关联工程"      // 未打开工程时的汇总名
	WeldGroupPersonDay = "person-day" // 按人、路径、日
	WeldGroupPerson    = "person"     // 按人
	WeldGroupProject   = "project"    // 按工程
	WeldGroupOrg       = "org"        // 按发生组织
	WeldGroupDay       = "day"        // 按 UTC 日
)

// WeldFact 是一次起停焊的运行事实：焊长、时长跟人走，工程名按当时快照。
type WeldFact struct {
	ID          uuid.UUID  // 产生端事实身份
	CreatorID   uuid.UUID  // 当时登录人
	FactoryID   uuid.UUID  // 所属工厂
	OrgUnitID   *uuid.UUID // 发生节点；厂直属为空
	OrgPath     []PathNode // 发生时路径快照
	ClientID    *uuid.UUID // 当时 Client；未匹配可空
	ProjectID   *uuid.UUID // 当时工程身份；未打开可空
	ProjectName string     // 当时工程名快照
	WeldKind    string     // single / multilayer / tbar / 空
	LengthMM    int64      // 本段焊长毫米
	DurationSec int64      // 本段时长秒
	OccurredAt  time.Time  // 焊完时间
	CreatedAt   time.Time  // 首次汇聚时间
}

// WeldFactView 是带当时人名的焊事实，供报表和下钻。
type WeldFactView struct {
	WeldFact
	LoginName   string // 当时登录名，可改
	DisplayName string // 显示名，可改
}

// WeldTotals 是某人已汇聚合计。
type WeldTotals struct {
	LengthMM    int64 `gorm:"column:length_mm"`    // 已汇聚焊长毫米
	DurationSec int64 `gorm:"column:duration_sec"` // 已汇聚时长秒
	RunCount    int64 `gorm:"column:run_count"`    // 已汇聚次数
}

// WeldProjectAgg 是某人按工程的合计。
type WeldProjectAgg struct {
	ProjectID   *uuid.UUID // 工程身份；未打开可空
	ProjectName string     // 汇总用工程名
	RunCount    int64      // 该工程次数
	LengthMM    int64      // 该工程焊长毫米
	DurationSec int64      // 该工程时长秒
}

// WeldWANSummary 是上送 WAN 的一粒：日、工程名、模式和合计，不含人员组织。
type WeldWANSummary struct {
	Day         string // UTC 日期 YYYY-MM-DD
	ProjectName string // 工程名快照或未关联
	WeldKind    string // single / multilayer / tbar / 空
	RunCount    int64  // 该粒次数
	LengthMM    int64  // 该粒焊长毫米
	DurationSec int64  // 该粒时长秒
}

// WeldReportRow 是按选定维度汇总的一行。
type WeldReportRow struct {
	CreatorID   uuid.UUID  // 当时登录人；非按人维为零值
	LoginName   string     // 登录名
	DisplayName string     // 显示名
	OrgUnitID   *uuid.UUID // 发生节点；厂直属为空
	OrgPath     []PathNode // 发生时路径
	Day         time.Time  // UTC 日期；非按日维为零值
	ProjectID   *uuid.UUID // 工程身份
	ProjectName string     // 工程名快照或未关联
	LengthMM    int64      // 该组焊长毫米
	DurationSec int64      // 该组时长秒
	RunCount    int64      // 该组次数
}

// WeldFactFilter 是焊事实查询窗；空字段表示不裁。
type WeldFactFilter struct {
	From      *time.Time // 发生时间起，含
	To        *time.Time // 发生时间止，不含
	CreatorID *uuid.UUID // 当时登录人
	ProjectID *uuid.UUID // 工程身份
	NoProject bool       // 只取未打开工程
	OrgUnitID *uuid.UUID // 发生节点
	Day       *time.Time // UTC 日历日
	Limit     int        // 条数上限；0 不裁
}

type weldFactRow struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey"`         // 产生端事实身份
	CreatorID   uuid.UUID  `gorm:"type:uuid;not null"`           // 当时登录人
	FactoryID   uuid.UUID  `gorm:"type:uuid;not null"`           // 所属工厂
	OrgUnitID   *uuid.UUID `gorm:"type:uuid"`                    // 发生节点
	OrgPath     []byte     `gorm:"type:jsonb;not null"`          // 路径快照原文
	ClientID    *uuid.UUID `gorm:"type:uuid"`                    // 当时 Client
	ProjectID   *uuid.UUID `gorm:"type:uuid"`                    // 当时工程身份
	ProjectName string     `gorm:"column:project_name;not null"` // 当时工程名
	WeldKind    string     `gorm:"column:weld_kind;not null"`    // 焊接模式
	LengthMM    int64      `gorm:"column:length_mm;not null"`    // 焊长毫米
	DurationSec int64      `gorm:"column:duration_sec;not null"` // 时长秒
	OccurredAt  time.Time  `gorm:"not null"`                     // 焊完时间
	CreatedAt   time.Time  `gorm:"not null"`                     // 首次汇聚时间
}

func (weldFactRow) TableName() string { return "weld_facts" }

type weldFactScan struct {
	ID          uuid.UUID  `gorm:"column:id"`           // 事实身份
	CreatorID   uuid.UUID  `gorm:"column:creator_id"`   // 当时登录人
	LoginName   string     `gorm:"column:login_name"`   // 登录名
	DisplayName string     `gorm:"column:display_name"` // 显示名
	OrgUnitID   *uuid.UUID `gorm:"column:org_unit_id"`  // 发生节点
	OrgPath     []byte     `gorm:"column:org_path"`     // 路径快照
	ClientID    *uuid.UUID `gorm:"column:client_id"`    // Client
	ProjectID   *uuid.UUID `gorm:"column:project_id"`   // 工程身份
	ProjectName string     `gorm:"column:project_name"` // 工程名
	WeldKind    string     `gorm:"column:weld_kind"`    // 焊接模式
	LengthMM    int64      `gorm:"column:length_mm"`    // 焊长毫米
	DurationSec int64      `gorm:"column:duration_sec"` // 时长秒
	OccurredAt  time.Time  `gorm:"column:occurred_at"`  // 焊完时间
	CreatedAt   time.Time  `gorm:"column:created_at"`   // 汇聚时间
}

type weldProjectScan struct {
	ProjectID   *uuid.UUID `gorm:"column:project_id"`   // 工程身份
	ProjectName string     `gorm:"column:project_name"` // 汇总名
	RunCount    int64      `gorm:"column:run_count"`    // 次数
	LengthMM    int64      `gorm:"column:length_mm"`    // 焊长毫米
	DurationSec int64      `gorm:"column:duration_sec"` // 时长秒
}

// ValidWeldKind 是否允许的焊接模式。
func ValidWeldKind(kind string) bool {
	switch kind {
	case "", WeldKindSingle, WeldKindMultilayer, WeldKindTBar:
		return true
	default:
		return false
	}
}

// ProjectLabel 空工程名按未关联展示。
func ProjectLabel(name string) string {
	if name == "" {
		return WeldUnsetProject
	}
	return name
}

// MergeWeldFact 按产生端身份写入；已有且字段相同则原样返回。
func (s *Store) MergeWeldFact(ctx context.Context, in WeldFact) (WeldFact, error) {
	if in.ID == uuid.Nil || in.CreatorID == uuid.Nil {
		return WeldFact{}, domain.ErrNotFound
	}
	if in.LengthMM < 0 || in.DurationSec < 0 || !ValidWeldKind(in.WeldKind) {
		return WeldFact{}, domain.ErrForbidden
	}
	got, err := s.WeldFactByID(ctx, in.ID)
	if err == nil {
		// 已有行字段不同不能覆盖。
		if !weldFactSame(got, in) {
			return WeldFact{}, domain.ErrIntegrity
		}
		return got, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return WeldFact{}, err
	}
	raw, err := marshalPath(in.OrgPath)
	if err != nil {
		return WeldFact{}, err
	}
	occurred := in.OccurredAt.UTC()
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	row := weldFactRow{
		ID:          in.ID,
		CreatorID:   in.CreatorID,
		FactoryID:   s.factoryID,
		OrgUnitID:   in.OrgUnitID,
		OrgPath:     raw,
		ClientID:    in.ClientID,
		ProjectID:   in.ProjectID,
		ProjectName: in.ProjectName,
		WeldKind:    in.WeldKind,
		LengthMM:    in.LengthMM,
		DurationSec: in.DurationSec,
		OccurredAt:  occurred,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return s.MergeWeldFact(ctx, in)
		}
		if domain.IsForeignKeyViolation(err) {
			return WeldFact{}, domain.ErrNotFound
		}
		if domain.IsCheckViolation(err) {
			return WeldFact{}, domain.ErrForbidden
		}
		return WeldFact{}, err
	}
	return weldFromRow(row), nil
}

// WeldFactByID 按产生端身份取一条焊事实。
func (s *Store) WeldFactByID(ctx context.Context, id uuid.UUID) (WeldFact, error) {
	var row weldFactRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return WeldFact{}, domain.ErrNotFound
		}
		return WeldFact{}, err
	}
	return weldFromRow(row), nil
}

// WeldTotalsByCreator 合计某人已汇聚焊长、时长和次数。
func (s *Store) WeldTotalsByCreator(ctx context.Context, creatorID uuid.UUID) (WeldTotals, error) {
	var out WeldTotals
	err := s.db.WithContext(ctx).Model(&weldFactRow{}).
		Select("COALESCE(SUM(length_mm),0) AS length_mm, COALESCE(SUM(duration_sec),0) AS duration_sec, COUNT(*) AS run_count").
		Where("creator_id = ?", creatorID).
		Scan(&out).Error
	return out, err
}

// ListWeldProjectsByCreator 按工程合计某人已汇聚事实。
func (s *Store) ListWeldProjectsByCreator(ctx context.Context, creatorID uuid.UUID) ([]WeldProjectAgg, error) {
	var rows []weldProjectScan
	err := s.db.WithContext(ctx).Table("weld_facts").
		Select("project_id, CASE WHEN project_name = '' THEN '未关联工程' ELSE project_name END AS project_name, COUNT(*) AS run_count, COALESCE(SUM(length_mm),0) AS length_mm, COALESCE(SUM(duration_sec),0) AS duration_sec").
		Where("creator_id = ?", creatorID).
		Group("project_id, CASE WHEN project_name = '' THEN '未关联工程' ELSE project_name END").
		Order("length_mm DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]WeldProjectAgg, 0, len(rows))
	for _, r := range rows {
		out = append(out, WeldProjectAgg{
			ProjectID:   r.ProjectID,
			ProjectName: r.ProjectName,
			RunCount:    r.RunCount,
			LengthMM:    r.LengthMM,
			DurationSec: r.DurationSec,
		})
	}
	return out, nil
}

// ListWeldFacts 按窗列出焊事实，带当时人名；新的在前。
func (s *Store) ListWeldFacts(ctx context.Context, f WeldFactFilter) ([]WeldFactView, error) {
	q := s.db.WithContext(ctx).Table("weld_facts AS f").
		Select("f.id, f.creator_id, p.login_name, p.display_name, f.org_unit_id, f.org_path, f.client_id, f.project_id, f.project_name, f.weld_kind, f.length_mm, f.duration_sec, f.occurred_at, f.created_at").
		Joins("JOIN people p ON p.id = f.creator_id").
		Order("f.occurred_at DESC")
	if f.From != nil {
		q = q.Where("f.occurred_at >= ?", f.From.UTC())
	}
	if f.To != nil {
		q = q.Where("f.occurred_at < ?", f.To.UTC())
	}
	if f.CreatorID != nil {
		q = q.Where("f.creator_id = ?", *f.CreatorID)
	}
	if f.NoProject {
		q = q.Where("f.project_id IS NULL AND f.project_name = ''")
	} else if f.ProjectID != nil {
		q = q.Where("f.project_id = ?", *f.ProjectID)
	}
	if f.OrgUnitID != nil {
		q = q.Where("f.org_unit_id = ?", *f.OrgUnitID)
	}
	if f.Day != nil {
		day := f.Day.UTC().Format("2006-01-02")
		q = q.Where("((f.occurred_at AT TIME ZONE 'UTC')::date) = ?::date", day)
	}
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	var rows []weldFactScan
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]WeldFactView, 0, len(rows))
	for _, r := range rows {
		out = append(out, WeldFactView{
			WeldFact: WeldFact{
				ID:          r.ID,
				CreatorID:   r.CreatorID,
				FactoryID:   s.factoryID,
				OrgUnitID:   r.OrgUnitID,
				OrgPath:     unmarshalPath(r.OrgPath),
				ClientID:    r.ClientID,
				ProjectID:   r.ProjectID,
				ProjectName: r.ProjectName,
				WeldKind:    r.WeldKind,
				LengthMM:    r.LengthMM,
				DurationSec: r.DurationSec,
				OccurredAt:  r.OccurredAt,
				CreatedAt:   r.CreatedAt,
			},
			LoginName:   r.LoginName,
			DisplayName: r.DisplayName,
		})
	}
	return out, nil
}

// ListWeldReport 按维度汇总可见事实之前的全表窗；权限由应用服务再裁。
func (s *Store) ListWeldReport(ctx context.Context, from, to *time.Time, group string) ([]WeldReportRow, error) {
	facts, err := s.ListWeldFacts(ctx, WeldFactFilter{From: from, To: to})
	if err != nil {
		return nil, err
	}
	return AggregateWeldFacts(facts, group), nil
}

// AggregateWeldFacts 把事实按维度收成报表行。
func AggregateWeldFacts(facts []WeldFactView, group string) []WeldReportRow {
	if group == "" {
		group = WeldGroupPersonDay
	}
	type acc struct {
		row WeldReportRow
	}
	order := make([]string, 0)
	by := map[string]*acc{}
	for _, f := range facts {
		key, row := weldGroupKey(group, f)
		got, ok := by[key]
		if !ok {
			got = &acc{row: row}
			by[key] = got
			order = append(order, key)
		}
		got.row.LengthMM += f.LengthMM
		got.row.DurationSec += f.DurationSec
		got.row.RunCount++
	}
	out := make([]WeldReportRow, 0, len(order))
	for _, key := range order {
		out = append(out, by[key].row)
	}
	return out
}

// weldFromRow 把库行收成焊事实视图。
func weldFromRow(row weldFactRow) WeldFact {
	return WeldFact{
		ID:          row.ID,
		CreatorID:   row.CreatorID,
		FactoryID:   row.FactoryID,
		OrgUnitID:   row.OrgUnitID,
		OrgPath:     unmarshalPath(row.OrgPath),
		ClientID:    row.ClientID,
		ProjectID:   row.ProjectID,
		ProjectName: row.ProjectName,
		WeldKind:    row.WeldKind,
		LengthMM:    row.LengthMM,
		DurationSec: row.DurationSec,
		OccurredAt:  row.OccurredAt,
		CreatedAt:   row.CreatedAt,
	}
}

// weldFactSame 已有行与待汇聚是否同一份，不同则拒绝覆盖。
func weldFactSame(got WeldFact, in WeldFact) bool {
	if got.CreatorID != in.CreatorID || !sameOptUUID(got.OrgUnitID, in.OrgUnitID) || !pathEqual(got.OrgPath, in.OrgPath) {
		return false
	}
	if !sameOptUUID(got.ClientID, in.ClientID) || !sameOptUUID(got.ProjectID, in.ProjectID) {
		return false
	}
	if got.ProjectName != in.ProjectName || got.WeldKind != in.WeldKind {
		return false
	}
	if got.LengthMM != in.LengthMM || got.DurationSec != in.DurationSec {
		return false
	}
	if in.OccurredAt.IsZero() {
		return true
	}
	return got.OccurredAt.UTC().Truncate(time.Second).Equal(in.OccurredAt.UTC().Truncate(time.Second))
}

// weldGroupKey 算出汇总键和空合计行。
func weldGroupKey(group string, f WeldFactView) (string, WeldReportRow) {
	day := f.OccurredAt.UTC().Truncate(24 * time.Hour)
	label := ProjectLabel(f.ProjectName)
	switch group {
	case WeldGroupPerson:
		return f.CreatorID.String(), WeldReportRow{
			CreatorID: f.CreatorID, LoginName: f.LoginName, DisplayName: f.DisplayName,
		}
	case WeldGroupProject:
		pid := ""
		if f.ProjectID != nil {
			pid = f.ProjectID.String()
		}
		return pid + "|" + label, WeldReportRow{ProjectID: f.ProjectID, ProjectName: label}
	case WeldGroupOrg:
		oid := "direct"
		if f.OrgUnitID != nil {
			oid = f.OrgUnitID.String()
		}
		return oid, WeldReportRow{OrgUnitID: f.OrgUnitID, OrgPath: f.OrgPath}
	case WeldGroupDay:
		return day.Format("2006-01-02"), WeldReportRow{Day: day}
	default:
		oid := "direct"
		if f.OrgUnitID != nil {
			oid = f.OrgUnitID.String()
		}
		key := f.CreatorID.String() + "|" + oid + "|" + day.Format("2006-01-02")
		return key, WeldReportRow{
			CreatorID: f.CreatorID, LoginName: f.LoginName, DisplayName: f.DisplayName,
			OrgUnitID: f.OrgUnitID, OrgPath: f.OrgPath, Day: day,
		}
	}
}

type weldWANScan struct {
	Day         string `gorm:"column:day"`          // UTC 日期
	ProjectName string `gorm:"column:project_name"` // 工程名
	WeldKind    string `gorm:"column:weld_kind"`    // 焊接模式
	RunCount    int64  `gorm:"column:run_count"`    // 次数
	LengthMM    int64  `gorm:"column:length_mm"`    // 焊长毫米
	DurationSec int64  `gorm:"column:duration_sec"` // 时长秒
}

// ListWeldWANSummaries 按日、工程名、模式合计全厂焊事实，不含人员组织。
func (s *Store) ListWeldWANSummaries(ctx context.Context) ([]WeldWANSummary, error) {
	var rows []weldWANScan
	err := s.db.WithContext(ctx).Table("weld_facts").
		Select(`to_char(((occurred_at AT TIME ZONE 'UTC')::date), 'YYYY-MM-DD') AS day,
			CASE WHEN project_name = '' THEN '未关联工程' ELSE project_name END AS project_name,
			weld_kind,
			COUNT(*) AS run_count,
			COALESCE(SUM(length_mm),0) AS length_mm,
			COALESCE(SUM(duration_sec),0) AS duration_sec`).
		Group(`((occurred_at AT TIME ZONE 'UTC')::date), CASE WHEN project_name = '' THEN '未关联工程' ELSE project_name END, weld_kind`).
		Order("day DESC, project_name, weld_kind").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]WeldWANSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, WeldWANSummary{
			Day: r.Day, ProjectName: r.ProjectName, WeldKind: r.WeldKind,
			RunCount: r.RunCount, LengthMM: r.LengthMM, DurationSec: r.DurationSec,
		})
	}
	return out, nil
}
