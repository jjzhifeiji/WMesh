package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

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
	WeldGroupOrg       = "org"        // 按人现在的所属组织
	WeldGroupDay       = "day"        // 按报表时区的日
	WeldMaxLengthMM    = 10_000_000   // 单条焊长上限毫米，即 10 公里
	WeldMaxDurationSec = 86_400       // 单条时长上限秒，即一整天
	WeldMaxProjectName = 200          // 工程名去首尾空白后的字数上限，和 WAN 一致
)

// WeldFact 是一次起停焊的运行事实：只绑人，组织按人现在的所属组织算；工程名按当时快照。
type WeldFact struct {
	ID          uuid.UUID  // 产生端事实身份
	CreatorID   uuid.UUID  // 当时登录人
	FactoryID   uuid.UUID  // 所属工厂
	ClientID    *uuid.UUID // 当时 Client；未匹配可空
	ProjectID   *uuid.UUID // 当时工程身份；未打开可空
	ProjectName string     // 当时工程名快照
	WeldKind    string     // single / multilayer / tbar / 空
	LengthMM    int64      // 本段焊长毫米
	DurationSec int64      // 本段时长秒
	OccurredAt  time.Time  // 焊完时间
	CreatedAt   time.Time  // 首次汇聚时间
}

// WeldFactView 是带人名和人现在所属组织的焊事实，供报表和下钻。
type WeldFactView struct {
	WeldFact               // 焊事实本体，人名另挂
	LoginName   string     // 当时登录名，可改
	DisplayName string     // 显示名，可改
	OrgUnitID   *uuid.UUID // 此人现在的所属组织；厂直属为空
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
	OrgUnitID   *uuid.UUID // 人现在的所属组织；厂直属为空
	OrgPath     []PathNode // 该组织当前路径，由应用服务按组织树补
	Day         time.Time  // 报表时区当日零点；非按日维为零值
	ProjectID   *uuid.UUID // 工程身份
	ProjectName string     // 工程名快照或未关联
	LengthMM    int64      // 该组焊长毫米
	DurationSec int64      // 该组时长秒
	RunCount    int64      // 该组次数
}

// WeldScope 是调用方能看的焊事实范围：只看本人焊的。
type WeldScope struct {
	SelfID uuid.UUID // 调用方本人
}

// WeldFactFilter 是焊事实查询窗；空字段表示不裁。
type WeldFactFilter struct {
	From       *time.Time // 发生时间起，含
	To         *time.Time // 发生时间止，不含
	CreatorID  *uuid.UUID // 当时登录人
	ProjectID  *uuid.UUID // 工程身份
	NoProject  bool       // 只取未打开工程
	OrgUnitID  *uuid.UUID // 人现在的所属组织
	DirectOnly bool       // 只取厂直属的人，即所属组织为空
	Day        *time.Time // UTC 日历日
	Scope      *WeldScope // 可见范围；空表示全厂都能看
	Limit      int        // 条数上限；0 不裁
	Offset     int        // 跳过前几条，分页用
}

// 一次起停焊的落库行。
type weldFactRow struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey"`         // 产生端事实身份
	CreatorID   uuid.UUID  `gorm:"type:uuid;not null"`           // 当时登录人
	FactoryID   uuid.UUID  `gorm:"type:uuid;not null"`           // 所属工厂
	ClientID    *uuid.UUID `gorm:"type:uuid"`                    // 当时 Client
	ProjectID   *uuid.UUID `gorm:"type:uuid"`                    // 当时工程身份
	ProjectName string     `gorm:"column:project_name;not null"` // 当时工程名
	WeldKind    string     `gorm:"column:weld_kind;not null"`    // 焊接模式
	LengthMM    int64      `gorm:"column:length_mm;not null"`    // 焊长毫米
	DurationSec int64      `gorm:"column:duration_sec;not null"` // 时长秒
	OccurredAt  time.Time  `gorm:"not null"`                     // 焊完时间
	CreatedAt   time.Time  `gorm:"not null"`                     // 首次汇聚时间
}

// 指定落库表名，避免查询时按类型名去猜。
func (weldFactRow) TableName() string { return "weld_facts" }

// 焊事实联上当时人名后的扫描行。
type weldFactScan struct {
	ID          uuid.UUID  `gorm:"column:id"`           // 事实身份
	CreatorID   uuid.UUID  `gorm:"column:creator_id"`   // 当时登录人
	LoginName   string     `gorm:"column:login_name"`   // 登录名
	DisplayName string     `gorm:"column:display_name"` // 显示名
	OrgUnitID   *uuid.UUID `gorm:"column:org_unit_id"`  // 人现在的所属组织
	ClientID    *uuid.UUID `gorm:"column:client_id"`    // Client
	ProjectID   *uuid.UUID `gorm:"column:project_id"`   // 工程身份
	ProjectName string     `gorm:"column:project_name"` // 工程名
	WeldKind    string     `gorm:"column:weld_kind"`    // 焊接模式
	LengthMM    int64      `gorm:"column:length_mm"`    // 焊长毫米
	DurationSec int64      `gorm:"column:duration_sec"` // 时长秒
	OccurredAt  time.Time  `gorm:"column:occurred_at"`  // 焊完时间
	CreatedAt   time.Time  `gorm:"column:created_at"`   // 汇聚时间
}

// 按维度汇总焊事实的扫描行；这一维没选的列留零值。
type weldReportScan struct {
	CreatorID   uuid.UUID  `gorm:"column:creator_id"`   // 当时登录人
	LoginName   string     `gorm:"column:login_name"`   // 登录名
	DisplayName string     `gorm:"column:display_name"` // 显示名
	OrgUnitID   *uuid.UUID `gorm:"column:org_unit_id"`  // 人现在的所属组织
	LocalDay    string     `gorm:"column:local_day"`    // 报表时区日期
	ProjectID   *uuid.UUID `gorm:"column:project_id"`   // 工程身份
	ProjectName string     `gorm:"column:project_name"` // 工程名或未关联
	RunCount    int64      `gorm:"column:run_count"`    // 次数
	LengthMM    int64      `gorm:"column:length_mm"`    // 焊长毫米，已封顶
	DurationSec int64      `gorm:"column:duration_sec"` // 时长秒，已封顶
}

// 按工程汇总焊事实的扫描行。
type weldProjectScan struct {
	ProjectID   *uuid.UUID `gorm:"column:project_id"`   // 工程身份
	ProjectName string     `gorm:"column:project_name"` // 汇总名
	RunCount    int64      `gorm:"column:run_count"`    // 次数
	LengthMM    int64      `gorm:"column:length_mm"`    // 焊长毫米
	DurationSec int64      `gorm:"column:duration_sec"` // 时长秒
}

// ValidWeldKind 是否允许的焊接模式。
func ValidWeldKind(kind string) bool {
	// 只放行空、单层、多层和 T 排。
	switch kind {
	// 这几种作业类型允许写入。
	case "", WeldKindSingle, WeldKindMultilayer, WeldKindTBar:
		return true
	// 其余字样拒绝，避免非法作业类型进库。
	default:
		return false
	}
}

// ValidWeldAmounts 单条焊长、时长是否在零到物理上限之间。
func ValidWeldAmounts(lengthMM, durationSec int64) bool {
	// 负数或超过上限都拒收，极大值会撑爆汇总
	return lengthMM >= 0 && durationSec >= 0 && lengthMM <= WeldMaxLengthMM && durationSec <= WeldMaxDurationSec
}

// ValidWeldProjectName 工程名去首尾空白后是否不超过上限。
func ValidWeldProjectName(name string) bool {
	// 按 WAN 口径先去空白再数字数，超长 WAN 会整批拒收
	return utf8.RuneCountInString(strings.TrimSpace(name)) <= WeldMaxProjectName
}

// cappedSum 是封顶求和表达式：空集算零，超过 int64 封到上限，扫描不再溢出。
func cappedSum(col string) string {
	// 列名只来自本包写死的字样，不拼外部输入
	return fmt.Sprintf("LEAST(COALESCE(SUM(%s),0), %d)::bigint", col, int64(math.MaxInt64))
}

// ProjectLabel 空工程名按未关联展示。
func ProjectLabel(name string) string {
	// 空名字不落库，避免出现空白项。
	if name == "" {
		return WeldUnsetProject
	}
	return name
}

// MergeWeldFact 按产生端身份写入；已有且字段相同则原样返回。
func (s *Store) MergeWeldFact(ctx context.Context, in WeldFact) (WeldFact, error) {
	// 没有身份就不能当有效目标。
	if in.ID == uuid.Nil || in.CreatorID == uuid.Nil {
		return WeldFact{}, domain.ErrNotFound
	}
	// 焊长时长超界或作业类型不认识就拒收。
	if !ValidWeldAmounts(in.LengthMM, in.DurationSec) || !ValidWeldKind(in.WeldKind) {
		return WeldFact{}, domain.ErrForbidden
	}
	// 工程名去空白后超长就拒收，免得 WAN 整批拒收。
	if !ValidWeldProjectName(in.ProjectName) {
		return WeldFact{}, domain.ErrInvalidName
	}
	// 先按身份读事实，结果留给紧跟着的判断。
	got, err := s.WeldFactByID(ctx, in.ID)
	// 已经查到就按现有结果核对，不再插入。
	if err == nil {
		// 已有行字段不同不能覆盖。
		if !weldFactSame(got, in) {
			return WeldFact{}, domain.ErrIntegrity
		}
		return got, nil
	}
	// 不是找不到的错误要原样交回。
	if !errors.Is(err, domain.ErrNotFound) {
		return WeldFact{}, err
	}
	// 先转到世界时，结果留给紧跟着的判断。
	occurred := in.OccurredAt.UTC()
	// 还没有更新时间就补上现在，避免零时间。
	if occurred.IsZero() {
		// 取当前时刻，时间列和租约用同一个时钟。
		occurred = time.Now().UTC()
	}
	// 按入参组装要写入的行。
	row := weldFactRow{
		ID:          in.ID,
		CreatorID:   in.CreatorID,
		FactoryID:   s.factoryID,
		ClientID:    in.ClientID,
		ProjectID:   in.ProjectID,
		ProjectName: in.ProjectName,
		WeldKind:    in.WeldKind,
		LengthMM:    in.LengthMM,
		DurationSec: in.DurationSec,
		OccurredAt:  occurred,
		CreatedAt:   time.Now().UTC(),
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			// 做完汇聚焊事实后把结果交回。
			return s.MergeWeldFact(ctx, in)
		}
		// 外键对不上就当被指的对象不存在。
		if domain.IsForeignKeyViolation(err) {
			return WeldFact{}, domain.ErrNotFound
		}
		// 检查约束不通过就改成业务拒绝。
		if domain.IsCheckViolation(err) {
			return WeldFact{}, domain.ErrForbidden
		}
		return WeldFact{}, err
	}
	// 库行收成对外结果再交回。
	return weldFromRow(row), nil
}

// WeldFactByID 按产生端身份取一条焊事实。
func (s *Store) WeldFactByID(ctx context.Context, id uuid.UUID) (WeldFact, error) {
	// 准备承接查到的焊事实。
	var row weldFactRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return WeldFact{}, domain.ErrNotFound
		}
		return WeldFact{}, err
	}
	// 库行收成对外结果再交回。
	return weldFromRow(row), nil
}

// WeldTotalsByCreator 合计某人已汇聚焊长、时长和次数。
func (s *Store) WeldTotalsByCreator(ctx context.Context, creatorID uuid.UUID) (WeldTotals, error) {
	// 准备承接这次要交回的结果。
	var out WeldTotals
	// 先接上要操作的表，结果留给紧跟着的判断。
	err := s.db.WithContext(ctx).Model(&weldFactRow{}).
		Select(cappedSum("length_mm")+" AS length_mm, "+cappedSum("duration_sec")+" AS duration_sec, COUNT(*) AS run_count").
		Where("creator_id = ?", creatorID).
		Scan(&out).Error
	return out, err
}

// ListWeldProjectsByCreator 按工程合计某人已汇聚事实。
func (s *Store) ListWeldProjectsByCreator(ctx context.Context, creatorID uuid.UUID) ([]WeldProjectAgg, error) {
	// 准备承接查到的那一行。
	var rows []weldProjectScan
	// 先带上请求上下文查库，结果留给紧跟着的判断。
	err := s.db.WithContext(ctx).Table("weld_facts").
		Select("project_id, CASE WHEN project_name = '' THEN '未关联工程' ELSE project_name END AS project_name, COUNT(*) AS run_count, "+
			cappedSum("length_mm")+" AS length_mm, "+cappedSum("duration_sec")+" AS duration_sec").
		Where("creator_id = ?", creatorID).
		Group("project_id, CASE WHEN project_name = '' THEN '未关联工程' ELSE project_name END").
		Order("length_mm DESC").
		Scan(&rows).Error
	// 出错就停，避免把失败当成已经完成。
	if err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]WeldProjectAgg, 0, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, r := range rows {
		// 收进结果，保持原来的先后顺序。
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

// weldFactsWithOrg 是焊事实连上人现在所属组织的底表，别名固定为 f。
const weldFactsWithOrg = "(SELECT wf.*, wp.org_unit_id FROM weld_facts wf JOIN people wp ON wp.id = wf.creator_id) AS f"

// weldFactWhere 把查询窗和可见范围拼进条件；表别名固定为 f，明细、合计和报表共用。
func weldFactWhere(q *gorm.DB, f WeldFactFilter) *gorm.DB {
	// 给了起始时间才按发生时间裁。
	if f.From != nil {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Where("f.occurred_at >= ?", f.From.UTC())
	}
	// 给了结束时间才把更晚的裁掉。
	if f.To != nil {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Where("f.occurred_at < ?", f.To.UTC())
	}
	// 指定了人就只取这个人的焊。
	if f.CreatorID != nil {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Where("f.creator_id = ?", *f.CreatorID)
	}
	// 只要没打开工程的那些焊。
	if f.NoProject {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Where("f.project_id IS NULL AND f.project_name = ''")
		// 点了工程就按工程筛，不把未关联混进来。
	} else if f.ProjectID != nil {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Where("f.project_id = ?", *f.ProjectID)
	}
	// 只要厂直属就取所属组织为空的人，指定节点不再看。
	if f.DirectOnly {
		// 厂直属的人没有所属组织。
		q = q.Where("f.org_unit_id IS NULL")
		// 点了节点就只取归在这个节点的人焊的。
	} else if f.OrgUnitID != nil {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Where("f.org_unit_id = ?", *f.OrgUnitID)
	}
	// 指定了日历日就只取那一天。
	if f.Day != nil {
		// 把焊完时间截到当天，汇总按天并。
		day := f.Day.UTC().Format("2006-01-02")
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Where("((f.occurred_at AT TIME ZONE 'UTC')::date) = ?::date", day)
	}
	// 带了可见范围就只留本人焊的。
	if f.Scope != nil {
		// 只留本人焊的，别人的一条不给。
		q = q.Where("f.creator_id = ?", f.Scope.SelfID)
	}
	return q
}

// ListWeldFacts 按窗列出焊事实，带当时人名；新的在前。
func (s *Store) ListWeldFacts(ctx context.Context, f WeldFactFilter) ([]WeldFactView, error) {
	// 补上筛选或限定列，避免动到不该动的字段。
	q := s.db.WithContext(ctx).Table(weldFactsWithOrg).
		Select("f.id, f.creator_id, p.login_name, p.display_name, f.org_unit_id, f.client_id, f.project_id, f.project_name, f.weld_kind, f.length_mm, f.duration_sec, f.occurred_at, f.created_at").
		Joins("JOIN people p ON p.id = f.creator_id").
		Order("f.occurred_at DESC, f.id DESC")
	// 拼上查询窗和可见范围，分页前先裁。
	q = weldFactWhere(q, f)
	// 给了跳过条数才往后翻，分页用。
	if f.Offset > 0 {
		// 跳过前面几页，排序固定翻页才不乱。
		q = q.Offset(f.Offset)
	}
	// 给了条数上限才截断。
	if f.Limit > 0 {
		// 补上筛选或限定列，避免动到不该动的字段。
		q = q.Limit(f.Limit)
	}
	// 准备承接查到的那一行。
	var rows []weldFactScan
	// 扫描查询结果失败就停，避免带着错误继续。
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]WeldFactView, 0, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, r := range rows {
		// 收进结果，保持原来的先后顺序。
		out = append(out, WeldFactView{
			WeldFact: WeldFact{
				ID:          r.ID,
				CreatorID:   r.CreatorID,
				FactoryID:   s.factoryID,
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
			OrgUnitID:   r.OrgUnitID,
		})
	}
	return out, nil
}

// WeldFactTotals 按同一查询窗合计条数、焊长和时长，不受分页影响；求和封顶。
func (s *Store) WeldFactTotals(ctx context.Context, f WeldFactFilter) (WeldTotals, error) {
	// 准备承接这次要交回的合计。
	var out WeldTotals
	// 只数条数和封顶求和，人名不用连表。
	q := s.db.WithContext(ctx).Table(weldFactsWithOrg).
		Select(cappedSum("f.length_mm") + " AS length_mm, " + cappedSum("f.duration_sec") + " AS duration_sec, COUNT(*) AS run_count")
	// 拼上和明细同一套查询窗与可见范围，再扫出合计。
	err := weldFactWhere(q, f).Scan(&out).Error
	return out, err
}

// ListWeldReport 在库里按维度汇总查询窗内的焊事实；按日维在 loc 时区切日，最近焊过的组在前。
func (s *Store) ListWeldReport(ctx context.Context, f WeldFactFilter, group string, loc *time.Location) ([]WeldReportRow, error) {
	// 没指定维度就按人、路径和日汇总。
	if group == "" {
		// 没指定维度就按人、路径和日期来汇总。
		group = WeldGroupPersonDay
	}
	// 没给时区就按 UTC 切日，和旧口径一致。
	if loc == nil {
		// 空时区退回 UTC，免得解引用空指针。
		loc = time.UTC
	}
	// 子查询先裁窗、换未关联名、算当地日期，外层只管分组。
	inner := weldFactWhere(s.db.Table(weldFactsWithOrg).
		Select(`f.id, f.creator_id, f.org_unit_id, f.project_id,
			CASE WHEN f.project_name = '' THEN '未关联工程' ELSE f.project_name END AS project_name,
			f.length_mm, f.duration_sec, f.occurred_at,
			to_char((f.occurred_at AT TIME ZONE ?), 'YYYY-MM-DD') AS local_day`, loc.String()), f)
	// 按维度取出分组列和要带出的列。
	keys, cols, withPeople := weldReportColumns(group)
	// 外层分组求和，求和用封顶表达式免得溢出。
	q := s.db.WithContext(ctx).Table("(?) AS g", inner).
		Select(cols + ", COUNT(*) AS run_count, " + cappedSum("g.length_mm") + " AS length_mm, " + cappedSum("g.duration_sec") + " AS duration_sec")
	// 带人名的维度连上人员表，名字取现在的。
	if withPeople {
		// 按登录人连上人员表。
		q = q.Joins("JOIN people p ON p.id = g.creator_id")
	}
	// 准备承接每组的合计行。
	var rows []weldReportScan
	// 按维度分组，最近焊过的组在前，同时刻再按分组列定序。
	if err := q.Group(keys).Order("MAX(g.occurred_at) DESC, " + keys).Scan(&rows).Error; err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]WeldReportRow, 0, len(rows))
	// 逐组收成报表行，保持库里排好的先后。
	for _, r := range rows {
		// 没选的列是零值，交回时照样留空。
		row := WeldReportRow{
			CreatorID: r.CreatorID, LoginName: r.LoginName, DisplayName: r.DisplayName,
			OrgUnitID: r.OrgUnitID, ProjectID: r.ProjectID, ProjectName: r.ProjectName,
			LengthMM: r.LengthMM, DurationSec: r.DurationSec, RunCount: r.RunCount,
		}
		// 选了日期列才按报表时区收成当地零点。
		if r.LocalDay != "" {
			// 日期文本按报表时区解析成当地零点。
			day, err := time.ParseInLocation("2006-01-02", r.LocalDay, loc)
			// 库里给的日期解析不了就停，不交错日。
			if err != nil {
				return nil, err
			}
			// 记下这一组的当地日期。
			row.Day = day
		}
		// 收进结果，保持原来的先后顺序。
		out = append(out, row)
	}
	return out, nil
}

// weldReportColumns 按维度交出分组列、要选的列，以及要不要连人员表取名字。
func weldReportColumns(group string) (string, string, bool) {
	// 按选定维度给出分组列。
	switch group {
	// 按人汇总时每人一行，名字跟着人。
	case WeldGroupPerson:
		// 交出按人的分组列和选列。
		return "g.creator_id, p.login_name, p.display_name", "g.creator_id, p.login_name, p.display_name", true
	// 按工程汇总时每个工程身份和名字一行。
	case WeldGroupProject:
		// 交出按工程的分组列和选列。
		return "g.project_id, g.project_name", "g.project_id, g.project_name", false
	// 按人现在的所属组织汇总，厂直属单独一行。
	case WeldGroupOrg:
		// 交出按组织的分组列，路径由应用服务补。
		return "g.org_unit_id", "g.org_unit_id", false
	// 按报表时区的日历日汇总，同一天并成一行。
	case WeldGroupDay:
		// 交出按日的分组列和选列。
		return "g.local_day", "g.local_day", false
	// 没指定维度就按人、路径和日汇总。
	default:
		// 交出按人、所属组织和日的分组列。
		return "g.creator_id, p.login_name, p.display_name, g.org_unit_id, g.local_day",
			"g.creator_id, p.login_name, p.display_name, g.org_unit_id, g.local_day", true
	}
}

// weldFromRow 把库行收成焊事实视图。
func weldFromRow(row weldFactRow) WeldFact {
	return WeldFact{
		ID:          row.ID,
		CreatorID:   row.CreatorID,
		FactoryID:   row.FactoryID,
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
	// 人对不上就不是同一份焊。
	if got.CreatorID != in.CreatorID {
		return false
	}
	// 本机或工程对不上就不能覆盖。
	if !sameOptUUID(got.ClientID, in.ClientID) || !sameOptUUID(got.ProjectID, in.ProjectID) {
		return false
	}
	// 带了作业类型才改，空着表示保持原样。
	if got.ProjectName != in.ProjectName || got.WeldKind != in.WeldKind {
		return false
	}
	// 焊长或时长不同就不能当成同一份。
	if got.LengthMM != in.LengthMM || got.DurationSec != in.DurationSec {
		return false
	}
	// 还没有更新时间就补上现在，避免零时间。
	if in.OccurredAt.IsZero() {
		return true
	}
	// 做完转到世界时后把结果交回。
	return got.OccurredAt.UTC().Truncate(time.Second).Equal(in.OccurredAt.UTC().Truncate(time.Second))
}

// 上送云端的焊汇总扫描行，不含人员。
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
	// 准备承接查到的多条焊汇总。
	var rows []weldWANScan
	// 先带上请求上下文查库，结果留给紧跟着的判断。
	err := s.db.WithContext(ctx).Table("weld_facts").
		Select(`to_char(((occurred_at AT TIME ZONE 'UTC')::date), 'YYYY-MM-DD') AS day,
			CASE WHEN project_name = '' THEN '未关联工程' ELSE project_name END AS project_name,
			weld_kind,
			COUNT(*) AS run_count,
			` + cappedSum("length_mm") + ` AS length_mm,
			` + cappedSum("duration_sec") + ` AS duration_sec`).
		Group(`((occurred_at AT TIME ZONE 'UTC')::date), CASE WHEN project_name = '' THEN '未关联工程' ELSE project_name END, weld_kind`).
		Order("day DESC, project_name, weld_kind").
		Scan(&rows).Error
	// 出错就停，避免把失败当成已经完成。
	if err != nil {
		return nil, err
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]WeldWANSummary, 0, len(rows))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, r := range rows {
		// 收进结果，保持原来的先后顺序。
		out = append(out, WeldWANSummary{
			Day: r.Day, ProjectName: r.ProjectName, WeldKind: r.WeldKind,
			RunCount: r.RunCount, LengthMM: r.LengthMM, DurationSec: r.DurationSec,
		})
	}
	return out, nil
}

// ClientWeldUsage 是一台设备上的焊次概况：最近谁焊、什么时候、累计多少。
type ClientWeldUsage struct {
	LastPersonID uuid.UUID `gorm:"column:last_person_id"` // 最近一次焊的人
	LastAt       time.Time `gorm:"column:last_at"`        // 最近一次焊完时间
	LengthMM     int64     `gorm:"column:length_mm"`      // 累计焊长毫米，已封顶
	RunCount     int64     `gorm:"column:run_count"`      // 累计次数
}

// 按设备汇总焊次的扫描行。
type clientWeldScan struct {
	ClientID        uuid.UUID `gorm:"column:client_id"` // 设备身份
	ClientWeldUsage           // 该设备的概况
}

// ListClientWeldUsage 按设备汇总焊事实：最近谁焊、什么时候、累计焊长和次数；没焊过的设备不在表里。
func (s *Store) ListClientWeldUsage(ctx context.Context) (map[uuid.UUID]ClientWeldUsage, error) {
	// 准备承接每台设备一行。
	var rows []clientWeldScan
	// 只看带了设备的焊事实，按设备分组。
	err := s.db.WithContext(ctx).Table("weld_facts").
		Select(`client_id,
			(array_agg(creator_id ORDER BY occurred_at DESC, id DESC))[1] AS last_person_id,
			MAX(occurred_at) AS last_at,
			COUNT(*) AS run_count,
			` + cappedSum("length_mm") + ` AS length_mm`).
		Where("client_id IS NOT NULL").
		Group("client_id").
		Scan(&rows).Error
	// 出错就停，避免把失败当成已经完成。
	if err != nil {
		return nil, err
	}
	// 按设备身份收成表。
	out := make(map[uuid.UUID]ClientWeldUsage, len(rows))
	// 逐台记下概况。
	for _, r := range rows {
		// 记进表里，按设备身份取。
		out[r.ClientID] = r.ClientWeldUsage
	}
	return out, nil
}
