package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/store"
)

const maxWeldSkipsReported = 100 // 回执最多列出的跳过行，总数另给

// 单行不合规的英文原因，只回给厂端记日志，不中止整批。
var (
	errWeldBadDay      = errors.New("invalid day")           // 日期不是合法的 YYYY-MM-DD
	errWeldBadKind     = errors.New("invalid weld kind")     // 焊接模式不认识
	errWeldNegative    = errors.New("negative value")        // 次数、焊长或时长为负
	errWeldNameTooLong = errors.New("project name too long") // 工程名去空白后超过 200 字
)

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

// WeldSummarySkip 是整表上送里被跳过的一行：序号和英文原因。
type WeldSummarySkip struct {
	Index  int    `json:"index"`  // 该行在上送里的序号，从 0 起
	Reason string `json:"reason"` // 跳过的英文原因
}

// WeldSummaryReceipt 是整表上送的回执，好行已入库，坏行在这里列出。
type WeldSummaryReceipt struct {
	Skipped      []WeldSummarySkip `json:"skipped"`      // 前 100 条跳过的行
	SkippedCount int               `json:"skippedCount"` // 跳过的总行数
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

// PutFromFactory 用该厂当前全量汇总覆盖云端旧行，只交回成败不要回执。
func (s *Stats) PutFromFactory(ctx context.Context, factoryID uuid.UUID, rows []WeldSummaryIn) error {
	// 回执只给 HTTP 入口用，这里只看成败。
	_, err := s.PutFromFactoryReport(ctx, factoryID, rows)
	return err
}

// PutFromFactoryReport 用该厂全量汇总覆盖云端旧行；坏行只跳过并列进回执。
func (s *Stats) PutFromFactoryReport(ctx context.Context, factoryID uuid.UUID, rows []WeldSummaryIn) (WeldSummaryReceipt, error) {
	// 跳过列表先给空表，回执里不出现 null。
	receipt := WeldSummaryReceipt{Skipped: []WeldSummarySkip{}}
	// 身份是空就按未设置处理，避免写空号。
	if factoryID == uuid.Nil {
		// 汇总被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", "factory", audit.Deny)
		return receipt, domain.ErrUnauthorized
	}
	// 按数量先准备容器。
	clean := make([]store.WeldSummary, 0, len(rows))
	// 记下每个键在结果里的位置，同键好合并。
	seen := make(map[string]int, len(rows))
	// 逐行整理，坏行只跳过，不连累整批。
	for i, row := range rows {
		// 收成合法值，空或太长不要。
		item, err := normalizeWeldSummary(row)
		// 不合规就记进回执跳过，别的行照收。
		if err != nil {
			// 跳过总数照记，列表只留前面若干条。
			receipt.SkippedCount++
			// 列表没满才记这一行的序号和原因。
			if len(receipt.Skipped) < maxWeldSkipsReported {
				// 记下序号和英文原因，厂端据此记日志。
				receipt.Skipped = append(receipt.Skipped, WeldSummarySkip{Index: i, Reason: err.Error()})
			}
			continue
		}
		// 全零的一粒没有内容，静默跳过。
		if item == nil {
			continue
		}
		// 拼一个唯一键，名字已去首尾空白。
		key := item.Day + "\x00" + item.ProjectName + "\x00" + item.WeldKind
		// 同键已经有了就相加，名字只差空白的算一粒。
		if at, ok := seen[key]; ok {
			// 取出已收下的那一粒，就地累加。
			prev := &clean[at]
			// 次数相加，超出上限就封顶，免得违反约束整批失败。
			prev.RunCount = saturatingAdd(prev.RunCount, item.RunCount)
			// 焊长相加，同样封顶。
			prev.LengthMM = saturatingAdd(prev.LengthMM, item.LengthMM)
			// 时长相加，同样封顶。
			prev.DurationSec = saturatingAdd(prev.DurationSec, item.DurationSec)
			continue
		}
		// 记下这个键落在结果的哪一位，后面同键好合并。
		seen[key] = len(clean)
		// 把这一项接进结果。
		clean = append(clean, *item)
	}
	// 按厂整表替换，删掉的日子不再留在云端。
	if err := s.store.ReplaceWeldSummaries(ctx, factoryID, clean); err != nil {
		// 汇总被拒就留审计。
		_ = s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Deny)
		return receipt, err
	}
	// 整表已替换，坏行只在回执里交代。
	return receipt, s.audit(ctx, nil, nil, &factoryID, "put_weld_summaries", factoryID.String(), audit.Allow)
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

// normalizeWeldSummary 收下合法一粒；全零跳过，不合规交回英文原因。
func normalizeWeldSummary(row WeldSummaryIn) (*store.WeldSummary, error) {
	// 去掉多余空白或前后缀。
	day := strings.TrimSpace(row.Day)
	// 解析这段文本，失败就不能继续。
	parsed, err := time.Parse("2006-01-02", day)
	// 格式不对或公元零年就跳过，库里的日期类型不收零年。
	if err != nil || parsed.Format("2006-01-02") != day || parsed.Year() < 1 {
		return nil, errWeldBadDay
	}
	// 去掉多余空白或前后缀。
	name := strings.TrimSpace(row.ProjectName)
	// 空和有值走不同路，避免把空白写进名录。
	if name == "" {
		// 没填工程名就归到未关联，避免空白分组。
		name = store.WeldUnsetProject
	}
	// 超过库里的字数上限就跳过这一行，避免整批写库失败。
	if utf8.RuneCountInString(name) > 200 {
		return nil, errWeldNameTooLong
	}
	// 模式不认识就跳过这一行，避免违反库约束。
	if !store.ValidWeldKind(row.WeldKind) {
		return nil, errWeldBadKind
	}
	// 出现负数就跳过这一行，汇总不能倒着计。
	if row.RunCount < 0 || row.LengthMM < 0 || row.DurationSec < 0 {
		return nil, errWeldNegative
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

// saturatingAdd 两个非负数相加，超过 int64 上限就封顶。
func saturatingAdd(a, b int64) int64 {
	// 加了会越过上限就直接封顶，免得溢出成负数。
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}
