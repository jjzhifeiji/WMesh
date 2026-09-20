package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/id"
)

const (
	WeldKindSingle     = "single"     // 单层焊道
	WeldKindMultilayer = "multilayer" // 多层焊缝
	WeldKindTBar       = "tbar"       // T 排对接
	WeldUnsetProject   = "未关联工程"      // 未打开工程时的汇总名
)

// WeldSummary 是一家厂按日、工程名、模式上送的焊汇总，不含人员组织。
type WeldSummary struct {
	FactoryID   uuid.UUID // 上送工厂
	FactoryName string    // 名录显示名
	Day         string    // UTC 日期 YYYY-MM-DD
	ProjectName string    // 工程名快照或未关联
	WeldKind    string    // single / multilayer / tbar / 空
	RunCount    int64     // 该粒次数
	LengthMM    int64     // 该粒焊长毫米
	DurationSec int64     // 该粒时长秒
}

type weldSummaryRow struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`         // 汇总行稳定身份
	FactoryID   uuid.UUID `gorm:"type:uuid;not null"`           // 上送工厂
	Day         string    `gorm:"type:date;not null"`           // UTC 日历日
	ProjectName string    `gorm:"column:project_name;not null"` // 工程名快照
	WeldKind    string    `gorm:"column:weld_kind;not null"`    // 焊接模式
	RunCount    int64     `gorm:"column:run_count;not null"`    // 该粒次数
	LengthMM    int64     `gorm:"column:length_mm;not null"`    // 焊长毫米
	DurationSec int64     `gorm:"column:duration_sec;not null"` // 时长秒
	UpdatedAt   time.Time `gorm:"column:updated_at;not null"`   // 最近替换时间
}

func (weldSummaryRow) TableName() string { return "weld_summaries" }

type weldSummaryScan struct {
	FactoryID   uuid.UUID `gorm:"column:factory_id"`   // 上送工厂
	FactoryName string    `gorm:"column:factory_name"` // 名录显示名
	Day         string    `gorm:"column:day"`          // UTC 日期
	ProjectName string    `gorm:"column:project_name"` // 工程名
	WeldKind    string    `gorm:"column:weld_kind"`    // 焊接模式
	RunCount    int64     `gorm:"column:run_count"`    // 次数
	LengthMM    int64     `gorm:"column:length_mm"`    // 焊长毫米
	DurationSec int64     `gorm:"column:duration_sec"` // 时长秒
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

// ReplaceWeldSummaries 用该厂当前全量汇总覆盖旧行，避免删掉的日子留在云端。
func (s *Store) ReplaceWeldSummaries(ctx context.Context, factoryID uuid.UUID, rows []WeldSummary) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("factory_id = ?", factoryID).Delete(&weldSummaryRow{}).Error; err != nil {
			return err
		}
		for _, in := range rows {
			row := weldSummaryRow{
				ID: id.New(), FactoryID: factoryID, Day: in.Day, ProjectName: in.ProjectName,
				WeldKind: in.WeldKind, RunCount: in.RunCount, LengthMM: in.LengthMM,
				DurationSec: in.DurationSec, UpdatedAt: now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ListWeldSummaries 列出各厂上送汇总；窗按 UTC 日与查询区间是否相交裁。
func (s *Store) ListWeldSummaries(ctx context.Context, factoryID *uuid.UUID, from, to *time.Time) ([]WeldSummary, error) {
	q := s.db.WithContext(ctx).Table("weld_summaries AS w").
		Select("w.factory_id, f.name AS factory_name, to_char(w.day, 'YYYY-MM-DD') AS day, w.project_name, w.weld_kind, w.run_count, w.length_mm, w.duration_sec").
		Joins("JOIN factories f ON f.id = w.factory_id").
		Order("f.name, w.day DESC, w.project_name, w.weld_kind")
	if factoryID != nil {
		q = q.Where("w.factory_id = ?", *factoryID)
	}
	if from != nil {
		q = q.Where("((w.day + 1)::timestamp AT TIME ZONE 'UTC') > ?", from.UTC())
	}
	if to != nil {
		q = q.Where("(w.day::timestamp AT TIME ZONE 'UTC') < ?", to.UTC())
	}
	var rows []weldSummaryScan
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]WeldSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, WeldSummary{
			FactoryID: r.FactoryID, FactoryName: r.FactoryName, Day: r.Day,
			ProjectName: r.ProjectName, WeldKind: r.WeldKind, RunCount: r.RunCount,
			LengthMM: r.LengthMM, DurationSec: r.DurationSec,
		})
	}
	return out, nil
}
