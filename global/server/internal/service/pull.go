package service

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
)

// IndexForFactory 列出该厂当前该有的指令，不含正文。kind 空则工艺和工程都给。
func (s *Service) IndexForFactory(ctx context.Context, factoryID uuid.UUID, kind string) ([]Cmd, error) {
	// 按工厂名录处理。
	fac, err := s.store.FactoryByID(ctx, factoryID)
	// 没有这家厂或状态不对就拒绝。
	if err != nil {
		return nil, err
	}
	// 先放工厂治理状态，停用厂不再发别的。
	out := []Cmd{{
		Typ: CmdFactoryState, Status: fac.Status, Revision: fac.LifecycleRevision, ShortCode: fac.ShortCode, FactoryName: fac.Name,
	}}
	// 不是有效厂就只给状态，不再发业务指令。
	if fac.Status != FactoryActive {
		return out, nil
	}
	// 列出这一批供后面筛选。
	rows, err := s.store.ListClientsByFactory(ctx, factoryID)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return nil, err
	}
	// 逐行整理，坏的一行就整批拒绝。
	for _, row := range rows {
		// 把这一项接进结果。
		out = append(out, Cmd{
			Typ: CmdClientBind, ClientID: row.ID.String(), ClientName: row.Name,
			ClientShortCode: row.ShortCode, DeviceSerial: row.DeviceSerial,
			PublicKey: row.PublicKey, BindingRevision: row.BindingRevision,
		})
	}
	// 先留空，再按种类装上封闭包。
	var snaps []ClosureSnapshot
	// 空和有值走不同路，避免把空白写进名录。
	if kind == "" {
		// 组好这一包，失败就不能继续。
		snaps, err = s.Closure.PackAvailableForFactory(ctx, factoryID)
		// 前面不成立就走这里，避免两种结果混用。
	} else {
		// 组好这一包，失败就不能继续。
		snaps, err = s.Closure.PackKindForFactory(ctx, factoryID, kind)
	}
	// 这一步失败就停，避免留下半截。
	if err != nil {
		return nil, err
	}
	// 逐个封闭包收成指令，指令里不含正文。
	for _, snap := range snaps {
		// 把这一项接进结果。
		out = append(out, Cmd{Typ: CmdClosure, AssetID: snap.AssetID.String(), Revision: snap.Revision, Kind: snap.Kind})
	}
	// 没指定种类时，工艺和工程都要下发。
	kinds := []string{KindProcess, KindProject}
	// 按是不是工程决定要不要核对焊道和依赖。
	if kind == KindProcess || kind == KindProject {
		// 调用方指定了种类，就只下那一种。
		kinds = []string{kind}
	}
	// 按是不是工程决定要不要核对焊道和依赖。
	if kind == "" || kind == KindProcess || kind == KindProject {
		// 按种类各做一遍，一种失败就停。
		for _, knd := range kinds {
			// 读平台级的这一份。
			layout, err := s.store.PlatformFSLayout(ctx, knd)
			// 厂级数据不在这里查。
			if err != nil {
				return nil, err
			}
			// 编成字节再送出。
			raw, err := json.Marshal(layout)
			// 编不出就拒绝，不发送半截。
			if err != nil {
				return nil, err
			}
			// 把这一项接进结果。
			out = append(out, Cmd{Typ: CmdFSApply, Kind: knd, Snapshot: raw})
		}
	}
	// 按是不是工程决定要不要核对焊道和依赖。
	if kind == "" || kind == KindProcess || kind == KindProject {
		// 取出要交给该厂的快照。
		tpls, err := s.Templates.SnapshotsForFactory(ctx, factoryID)
		// 取不到快照就拒绝，残包不能发出去。
		if err != nil {
			return nil, err
		}
		// 逐份模版下发，种类不符的跳过。
		for _, t := range tpls {
			// 空和有值走不同路，避免把空白写进名录。
			if kind != "" && t.Kind != kind {
				continue
			}
			// 把这一项接进结果。
			out = append(out, Cmd{Typ: CmdTemplate, TemplateID: t.ID.String(), Kind: t.Kind, Revision: t.Revision})
		}
	}
	// 空和有值走不同路，避免把空白写进名录。
	if kind == "" {
		// 列出这一批供后面筛选。
		ids, err := s.ListRetractions(ctx)
		// 列出失败就拒绝，避免交出不完整结果。
		if err != nil {
			return nil, err
		}
		// 逐个身份处理，缺了就整次失败。
		for _, id := range ids {
			// 把这一项接进结果。
			out = append(out, Cmd{Typ: CmdRetract, AssetID: id.String()})
		}
	}
	return out, nil
}

// PullClosureForFactory 组并密封这一条给该厂。
func (s *Closure) PullClosureForFactory(ctx context.Context, factoryID, assetID uuid.UUID) (ClosureSnapshot, error) {
	// 组好这一包，失败就不能继续。
	snap, err := s.PackAssetForFactory(ctx, assetID, factoryID)
	// 成员不齐就拒绝，残包不能下发。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	// 没有组出包就按不存在，草稿不会下发。
	if snap.AssetID == uuid.Nil {
		return ClosureSnapshot{}, domain.ErrNotFound
	}
	// 封好再离开本侧。
	sealed, err := s.SealClosureTransit(ctx, factoryID, snap)
	// 封不上就拒绝下发，明文不能出站。
	if err != nil {
		// 借通道补发租约，封包失败才走这里。
		ch := Channel{s.kernel}
		// 租约补上了就再封一次，仍失败则拒绝下发。
		if _, lerr := ch.IssueContentLease(ctx, factoryID); lerr == nil {
			// 封好再离开本侧。
			sealed, err = s.SealClosureTransit(ctx, factoryID, snap)
		}
	}
	// 这一步失败就停，避免留下半截。
	if err != nil {
		return ClosureSnapshot{}, err
	}
	return sealed, nil
}

// PullTemplateForFactory 组这一份模版给该厂。
func (s *Templates) PullTemplateForFactory(ctx context.Context, factoryID, templateID uuid.UUID) (TemplateSnapshot, error) {
	// 取出要交给该厂的快照。
	return s.SnapshotForFactory(ctx, factoryID, templateID)
}
