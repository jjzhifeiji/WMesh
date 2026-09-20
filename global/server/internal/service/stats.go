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
type Stats struct{ *kernel }

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
	if factoryID == uuid.Nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", "factory", audit.Deny)
		return domain.ErrUnauthorized
	}
	if len(rows) > maxWeldSummaryRows {
		_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Deny)
		return domain.ErrForbidden
	}
	clean := make([]store.WeldSummary, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		item, err := normalizeWeldSummary(row)
		if err != nil {
			_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Deny)
			return err
		}
		if item == nil {
			continue
		}
		key := item.Day + "\x00" + item.ProjectName + "\x00" + item.WeldKind
		if _, ok := seen[key]; ok {
			_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Deny)
			return domain.ErrForbidden
		}
		seen[key] = struct{}{}
		clean = append(clean, *item)
	}
	// 按厂整表替换，删掉的日子不再留在云端。
	if err := s.store.ReplaceWeldSummaries(ctx, factoryID, clean); err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Allow)
}

// ListWeldReports 给 WAN 管理员看跨厂焊汇总，不含人员组织。
func (s *Stats) ListWeldReports(ctx context.Context, token, factoryID string, from, to *time.Time) ([]WeldSummaryView, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "list_weld_reports", "wan", audit.Deny)
		return nil, err
	}
	var fid *uuid.UUID
	if strings.TrimSpace(factoryID) != "" {
		id, err := uuid.Parse(factoryID)
		if err != nil {
			_ = s.audit(ctx, &admin.ID, nil, nil, "list_weld_reports", factoryID, audit.Deny)
			return nil, domain.ErrInvalidName
		}
		fid = &id
	}
	rows, err := s.store.ListWeldSummaries(ctx, fid, from, to)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, fid, "list_weld_reports", "wan", audit.Deny)
		return nil, err
	}
	out := make([]WeldSummaryView, 0, len(rows))
	for _, r := range rows {
		out = append(out, WeldSummaryView{
			FactoryID: r.FactoryID, FactoryName: r.FactoryName, Day: r.Day,
			ProjectName: r.ProjectName, WeldKind: r.WeldKind, RunCount: r.RunCount,
			LengthMM: r.LengthMM, DurationSec: r.DurationSec,
		})
	}
	return out, s.audit(ctx, &admin.ID, nil, fid, "list_weld_reports", "wan", audit.Allow)
}

// normalizeWeldSummary 收下合法一粒；全零跳过。
func normalizeWeldSummary(row WeldSummaryIn) (*store.WeldSummary, error) {
	day := strings.TrimSpace(row.Day)
	parsed, err := time.Parse("2006-01-02", day)
	if err != nil || parsed.Format("2006-01-02") != day {
		return nil, domain.ErrInvalidName
	}
	name := strings.TrimSpace(row.ProjectName)
	if name == "" {
		name = store.WeldUnsetProject
	}
	if utf8.RuneCountInString(name) > 200 {
		return nil, domain.ErrInvalidName
	}
	if !store.ValidWeldKind(row.WeldKind) {
		return nil, domain.ErrInvalidName
	}
	if row.RunCount < 0 || row.LengthMM < 0 || row.DurationSec < 0 {
		return nil, domain.ErrForbidden
	}
	if row.RunCount == 0 && row.LengthMM == 0 && row.DurationSec == 0 {
		return nil, nil
	}
	return &store.WeldSummary{
		Day: day, ProjectName: name, WeldKind: row.WeldKind,
		RunCount: row.RunCount, LengthMM: row.LengthMM, DurationSec: row.DurationSec,
	}, nil
}
