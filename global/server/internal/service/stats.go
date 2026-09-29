package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/store"
)

const maxWeldSummaryRows = 5000 // 一厂一次上送上限，挡住异常包

// Stats 管各厂上送的焊汇总，不含人员组织。
type Stats struct {
	*kernel // 焊汇总共用的库与审计。
}

// WeldSummaryIn 是厂端上送的一粒：日、工程名、模式和合计。
type WeldSummaryIn struct {
	Day         string `json:"day"`         // UTC 日期 YYYY-MM-DD
	ProjectName string `json:"projectName"` // 工程名快照；空则未关联
	WeldKind    string `json:"weldKind"`    // single / multilayer / tbar / 空
	RunCount    int64  `json:"runCount"`    // 该粒次数
	LengthMM    int64  `json:"lengthMm"`    // 该粒焊长毫米
	DurationSec int64  `json:"durationSec"` // 该粒时长秒
}

// WeldSummaryView 是给 WAN 管理员看的汇总行，不含人员组织。
type WeldSummaryView struct {
	FactoryID   uuid.UUID `json:"factoryId"`   // 上送工厂
	FactoryName string    `json:"factoryName"` // 名录显示名
	Day         string    `json:"day"`         // UTC 日期 YYYY-MM-DD
	ProjectName string    `json:"projectName"` // 工程名快照或未关联
	WeldKind    string    `json:"weldKind"`    // 焊接模式
	RunCount    int64     `json:"runCount"`    // 该粒次数
	LengthMM    int64     `json:"lengthMm"`    // 该粒焊长毫米
	DurationSec int64     `json:"durationSec"` // 该粒时长秒
}

// PutFromFactory 用该厂当前全量汇总覆盖云端旧行；不含人员组织字段。
func (s *Stats) PutFromFactory(ctx context.Context, factoryID uuid.UUID, rows []WeldSummaryIn) error {
	// 身份是空就按未设置处理，避免写空号。
	if factoryID == uuid.Nil {
		// 汇总被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", "factory", audit.Deny)
		return domain.ErrUnauthorized
	}
	// 超过一厂一次的上限则拒绝，挡住异常包。
	if len(rows) > maxWeldSummaryRows {
		// 汇总被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	// 按数量先准备容器。
	clean := make([]store.WeldSummary, 0, len(rows))
	// 按数量先准备容器。
	seen := make(map[string]struct{}, len(rows))
	// 逐行整理，坏的一行就整批拒绝。
	for _, row := range rows {
		// 收成合法值，空或太长不要。
		item, err := normalizeWeldSummary(row)
		// 不合法就拒绝，避免脏数据入库。
		if err != nil {
			// 汇总被拒就留审计。
			_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Deny)
			return err
		}
		// 还没有这条记录就新建或跳过，不能当已有。
		if item == nil {
			continue
		}
		// 拼一个唯一键，挡住同一项写两次。
		key := item.Day + "\x00" + item.ProjectName + "\x00" + item.WeldKind
		// 同一键已经见过则拒绝，防止写两遍。
		if _, ok := seen[key]; ok {
			// 汇总被拒就留审计。
			_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Deny)
			return domain.ErrForbidden
		}
		// 记下已见过的键，防止同一粒写两次。
		seen[key] = struct{}{}
		// 把这一项接进结果。
		clean = append(clean, *item)
	}
	// 按厂整表替换，删掉的日子不再留在云端。
	if err := s.store.ReplaceWeldSummaries(ctx, factoryID, clean); err != nil {
		// 汇总被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Deny)
		return err
	}
	// 超限或重复的整批不收。
	return s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Allow)
}

// ListWeldReports 给 WAN 管理员看跨厂焊汇总，不含人员组织。
func (s *Stats) ListWeldReports(ctx context.Context, token, factoryID string, from, to *time.Time) ([]WeldSummaryView, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 看汇总被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "list_weld_reports", "wan", audit.Deny)
		return nil, err
	}
	// 先不限定工厂，传入身份再收窄查询。
	var fid *uuid.UUID
	// 空和有值走不同路，避免把空白写进名录。
	if strings.TrimSpace(factoryID) != "" {
		// 解析这段文本，失败就不能继续。
		id, err := uuid.Parse(factoryID)
		// 格式不对就拒绝，不接收坏数据。
		if err != nil {
			// 看汇总被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, nil, "list_weld_reports", factoryID, audit.Deny)
			return nil, domain.ErrInvalidName
		}
		// 查询收窄到这一家厂。
		fid = &id
	}
	// 列出这一批供后面筛选。
	rows, err := s.store.ListWeldSummaries(ctx, fid, from, to)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		// 看汇总被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, fid, "list_weld_reports", "wan", audit.Deny)
		return nil, err
	}
	// 按数量先准备容器。
	out := make([]WeldSummaryView, 0, len(rows))
	// 逐行整理，坏的一行就整批拒绝。
	for _, r := range rows {
		// 把这一项接进结果。
		out = append(out, WeldSummaryView{
			FactoryID: r.FactoryID, FactoryName: r.FactoryName, Day: r.Day,
			ProjectName: r.ProjectName, WeldKind: r.WeldKind, RunCount: r.RunCount,
			LengthMM: r.LengthMM, DurationSec: r.DurationSec,
		})
	}
	// 读到后才记允许。
	return out, s.audit(ctx, &admin.ID, nil, fid, "list_weld_reports", "wan", audit.Allow)
}

// normalizeWeldSummary 收下合法一粒；全零跳过。
func normalizeWeldSummary(row WeldSummaryIn) (*store.WeldSummary, error) {
	// 去掉多余空白或前后缀。
	day := strings.TrimSpace(row.Day)
	// 解析这段文本，失败就不能继续。
	parsed, err := time.Parse("2006-01-02", day)
	// 格式不对就拒绝，不接收坏数据。
	if err != nil || parsed.Format("2006-01-02") != day {
		return nil, domain.ErrInvalidName
	}
	// 去掉多余空白或前后缀。
	name := strings.TrimSpace(row.ProjectName)
	// 空和有值走不同路，避免把空白写进名录。
	if name == "" {
		// 没填工程名就归到未关联，避免空白分组。
		name = store.WeldUnsetProject
	}
	// 字数不在允许范围就拒绝，避免空名或超长。
	if utf8.RuneCountInString(name) > 200 {
		return nil, domain.ErrInvalidName
	}
	// 条件不满足则拒绝，避免把错状态写进去。
	if !store.ValidWeldKind(row.WeldKind) {
		return nil, domain.ErrInvalidName
	}
	// 出现负数则整批拒绝，汇总不能倒着计。
	if row.RunCount < 0 || row.LengthMM < 0 || row.DurationSec < 0 {
		return nil, domain.ErrForbidden
	}
	// 次数、焊长、时长都是零就跳过这一粒。
	if row.RunCount == 0 && row.LengthMM == 0 && row.DurationSec == 0 {
		return nil, nil
	}
	return &store.WeldSummary{
		Day: day, ProjectName: name, WeldKind: row.WeldKind,
		RunCount: row.RunCount, LengthMM: row.LengthMM, DurationSec: row.DurationSec,
	}, nil
}
