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
	WeldFact           // 焊事实本体，人名另挂
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

// 一次起停焊的落库行。
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

// 指定落库表名，避免查询时按类型名去猜。
func (weldFactRow) TableName() string { return "weld_facts" }

// 焊事实联上当时人名后的扫描行。
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
	// 带了作业类型才改，空着表示保持原样。
	if in.LengthMM < 0 || in.DurationSec < 0 || !ValidWeldKind(in.WeldKind) {
		return WeldFact{}, domain.ErrForbidden
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
	// 先收成路径，结果留给紧跟着的判断。
	raw, err := marshalPath(in.OrgPath)
	// 收成路径失败就停，避免带着错误继续。
	if err != nil {
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
		Select("COALESCE(SUM(length_mm),0) AS length_mm, COALESCE(SUM(duration_sec),0) AS duration_sec, COUNT(*) AS run_count").
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
		Select("project_id, CASE WHEN project_name = '' THEN '未关联工程' ELSE project_name END AS project_name, COUNT(*) AS run_count, COALESCE(SUM(length_mm),0) AS length_mm, COALESCE(SUM(duration_sec),0) AS duration_sec").
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

// ListWeldFacts 按窗列出焊事实，带当时人名；新的在前。
func (s *Store) ListWeldFacts(ctx context.Context, f WeldFactFilter) ([]WeldFactView, error) {
	// 补上筛选或限定列，避免动到不该动的字段。
	q := s.db.WithContext(ctx).Table("weld_facts AS f").
		Select("f.id, f.creator_id, p.login_name, p.display_name, f.org_unit_id, f.org_path, f.client_id, f.project_id, f.project_name, f.weld_kind, f.length_mm, f.duration_sec, f.occurred_at, f.created_at").
		Joins("JOIN people p ON p.id = f.creator_id").
		Order("f.occurred_at DESC")
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
	// 挂了节点就要确认节点还在，避免悬空归属。
	if f.OrgUnitID != nil {
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
	// 先列出焊事实，结果留给紧跟着的判断。
	facts, err := s.ListWeldFacts(ctx, WeldFactFilter{From: from, To: to})
	// 列出焊事实失败就停，避免带着错误继续。
	if err != nil {
		return nil, err
	}
	// 把这一步的结果交回给调用方。
	return AggregateWeldFacts(facts, group), nil
}

// AggregateWeldFacts 把事实按维度收成报表行。
func AggregateWeldFacts(facts []WeldFactView, group string) []WeldReportRow {
	// 没指定维度就按人、路径和日期汇总。
	if group == "" {
		// 没指定维度就按人、路径和日期来汇总。
		group = WeldGroupPersonDay
	}
	// 临时累计一行报表，同一键加在一起。
	type acc struct {
		row WeldReportRow // 这一维度累计中的报表行
	}
	// 按需要预留位置，避免后面反复扩容。
	order := make([]string, 0)
	// 按首次出现的顺序累加，汇总时不打乱。
	by := map[string]*acc{}
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, f := range facts {
		// 准备汇总键或密钥缓冲区。
		key, row := weldGroupKey(group, f)
		// 看这一组有没有累计过。
		got, ok := by[key]
		// 这组还没有就新建一行再累加。
		if !ok {
			// 这是新的一组，放进累计里。
			got = &acc{row: row}
			// 记下新建的这一组，后面往里加。
			by[key] = got
			// 收进结果，保持原来的先后顺序。
			order = append(order, key)
		}
		// 把这段焊长加进当前这一组。
		got.row.LengthMM += f.LengthMM
		// 把这段时长加进当前这一组。
		got.row.DurationSec += f.DurationSec
		// 这一组的次数加一。
		got.row.RunCount++
	}
	// 按需要预留位置，避免后面反复扩容。
	out := make([]WeldReportRow, 0, len(order))
	// 逐条处理，避免漏掉还要读或还要写的行。
	for _, key := range order {
		// 收进结果，保持原来的先后顺序。
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
	// 人或路径对不上就不是同一份焊。
	if got.CreatorID != in.CreatorID || !sameOptUUID(got.OrgUnitID, in.OrgUnitID) || !pathEqual(got.OrgPath, in.OrgPath) {
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

// weldGroupKey 算出汇总键和空合计行。
func weldGroupKey(group string, f WeldFactView) (string, WeldReportRow) {
	// 把焊完时间截到当天，汇总按天并。
	day := f.OccurredAt.UTC().Truncate(24 * time.Hour)
	// 空工程名换成未关联，避免空白分组。
	label := ProjectLabel(f.ProjectName)
	// 按选定维度生成汇总键。
	switch group {
	// 按人汇总时每人一行。
	case WeldGroupPerson:
		// 交出这一维的键和空合计行。
		return f.CreatorID.String(), WeldReportRow{
			CreatorID: f.CreatorID, LoginName: f.LoginName, DisplayName: f.DisplayName,
		}
	// 按工程汇总时每个工程一行。
	case WeldGroupProject:
		// 记下这一层文件夹，用来还原从根到父的路径。
		pid := ""
		if f.ProjectID != nil {
			// 先收成文本，结果留给紧跟着的判断。
			pid = f.ProjectID.String()
		}
		return pid + "|" + label, WeldReportRow{ProjectID: f.ProjectID, ProjectName: label}
	// 按发生组织汇总。
	case WeldGroupOrg:
		// 厂直属先用固定字样，有节点再换成节点。
		oid := "direct"
		if f.OrgUnitID != nil {
			// 先收成文本，结果留给紧跟着的判断。
			oid = f.OrgUnitID.String()
		}
		return oid, WeldReportRow{OrgUnitID: f.OrgUnitID, OrgPath: f.OrgPath}
	// 按日历日汇总，同一天并成一行。
	case WeldGroupDay:
		// 交出这一维的键和空合计行。
		return day.Format("2006-01-02"), WeldReportRow{Day: day}
	// 没指定维度就按人、路径和日汇总。
	default:
		// 厂直属先用固定字样，有节点再换成节点。
		oid := "direct"
		if f.OrgUnitID != nil {
			// 先收成文本，结果留给紧跟着的判断。
			oid = f.OrgUnitID.String()
		}
		// 准备汇总键或密钥缓冲区。
		key := f.CreatorID.String() + "|" + oid + "|" + day.Format("2006-01-02")
		return key, WeldReportRow{
			CreatorID: f.CreatorID, LoginName: f.LoginName, DisplayName: f.DisplayName,
			OrgUnitID: f.OrgUnitID, OrgPath: f.OrgPath, Day: day,
		}
	}
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
			COALESCE(SUM(length_mm),0) AS length_mm,
			COALESCE(SUM(duration_sec),0) AS duration_sec`).
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
