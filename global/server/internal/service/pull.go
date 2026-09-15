package service

import (
	"context"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
)

// IndexForFactory 列出该厂当前该有的指令，不含正文。kind 空则工艺和工程都给。
func (s *Service) IndexForFactory(ctx context.Context, factoryID uuid.UUID, kind string) ([]Cmd, error) {
	fac, err := s.store.FactoryByID(ctx, factoryID)
	if err != nil {
		return nil, err
	}
	out := []Cmd{{
		Typ: CmdFactoryState, Status: fac.Status, Revision: fac.LifecycleRevision, ShortCode: fac.ShortCode,
	}}
	if fac.Status != FactoryActive {
		return out, nil
	}
	rows, err := s.store.ListClientsByFactory(ctx, factoryID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out = append(out, Cmd{
			Typ: CmdClientBind, ClientID: row.ID.String(), ClientName: row.Name,
			ClientShortCode: row.ShortCode, PublicKey: row.PublicKey, BindingRevision: row.BindingRevision,
		})
	}
	var snaps []ClosureSnapshot
	if kind == "" {
		snaps, err = s.Closure.PackAvailableForFactory(ctx, factoryID)
	} else {
		snaps, err = s.Closure.PackKindForFactory(ctx, factoryID, kind)
	}
	if err != nil {
		return nil, err
	}
	for _, snap := range snaps {
		out = append(out, Cmd{Typ: CmdClosure, AssetID: snap.AssetID.String(), Revision: snap.Revision, Kind: snap.Kind})
	}
	if kind == "" || kind == KindProcess || kind == KindProject {
		tpls, err := s.Templates.SnapshotsForFactory(ctx, factoryID)
		if err != nil {
			return nil, err
		}
		for _, t := range tpls {
			if kind != "" && t.Kind != kind {
				continue
			}
			out = append(out, Cmd{Typ: CmdTemplate, TemplateID: t.ID.String(), Kind: t.Kind, Revision: t.Revision})
		}
	}
	if kind == "" {
		// 只列还能拉到正文的当前版本，避免索引指向空包。
		snaps, err := s.Updates.SnapshotsForFactory(ctx, factoryID)
		if err != nil {
			return nil, err
		}
		latest := map[string]int64{}
		for _, snap := range snaps {
			if snap.Version > latest[snap.Kind] {
				latest[snap.Kind] = snap.Version
			}
		}
		for kind, version := range latest {
			out = append(out, Cmd{Typ: CmdSoftware, Kind: kind, Version: version})
		}
		ids, err := s.ListRetractions(ctx)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			out = append(out, Cmd{Typ: CmdRetract, AssetID: id.String()})
		}
	}
	return out, nil
}

// PullClosureForFactory 组并密封这一条给该厂。
func (s *Closure) PullClosureForFactory(ctx context.Context, factoryID, assetID uuid.UUID) (ClosureSnapshot, error) {
	snap, err := s.PackAssetForFactory(ctx, assetID, factoryID)
	if err != nil {
		return ClosureSnapshot{}, err
	}
	if snap.AssetID == uuid.Nil {
		return ClosureSnapshot{}, domain.ErrNotFound
	}
	sealed, err := s.SealClosureTransit(ctx, factoryID, snap)
	if err != nil {
		ch := Channel{s.kernel}
		if _, lerr := ch.IssueContentLease(ctx, factoryID); lerr == nil {
			sealed, err = s.SealClosureTransit(ctx, factoryID, snap)
		}
	}
	if err != nil {
		return ClosureSnapshot{}, err
	}
	return sealed, nil
}

// PullTemplateForFactory 组这一份模版给该厂。
func (s *Templates) PullTemplateForFactory(ctx context.Context, factoryID, templateID uuid.UUID) (TemplateSnapshot, error) {
	return s.SnapshotForFactory(ctx, factoryID, templateID)
}

// PullSoftwareForFactory 按已下发记录给出带正文的软件包。
func (s *Updates) PullSoftwareForFactory(ctx context.Context, factoryID uuid.UUID, kind string, version int64) (SoftwareSnapshot, error) {
	if !validSoftwareKind(kind) || version < 1 {
		return SoftwareSnapshot{}, domain.ErrNotFound
	}
	if _, err := s.store.SoftwareDistribution(ctx, kind, version, factoryID); err != nil {
		return SoftwareSnapshot{}, err
	}
	snaps, err := s.SnapshotsForFactory(ctx, factoryID)
	if err != nil {
		return SoftwareSnapshot{}, err
	}
	for _, snap := range snaps {
		if snap.Kind == kind && snap.Version == version {
			return snap, nil
		}
	}
	return SoftwareSnapshot{}, domain.ErrNotFound
}
