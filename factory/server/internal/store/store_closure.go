package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
)

// CacheLimit 读本厂 Client 工程缓存上限。
func (s *Store) CacheLimit(ctx context.Context) (int, error) {
	var row factorySettingsRow
	if err := s.db.WithContext(ctx).First(&row, "id = 1").Error; err != nil {
		return 0, err
	}
	return row.MaxCachedProjects, nil
}

// SetCacheLimit 写入本厂缓存上限，须 ≥1。
func (s *Store) SetCacheLimit(ctx context.Context, n int) error {
	if n < 1 {
		return domain.ErrForbidden
	}
	res := s.db.WithContext(ctx).Model(&factorySettingsRow{}).Where("id = 1").Update("max_cached_projects", n)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// InsertReplica 写入一条平台级副本；同身份修订且摘要相同则原样返回。
func (s *Store) InsertReplica(ctx context.Context, in AssetReplica) (AssetReplica, error) {
	if err := assertAssetDigest(in.Digest); err != nil {
		return AssetReplica{}, err
	}
	deps, err := marshalAssetDeps(in.Kind, in.Deps)
	if err != nil {
		return AssetReplica{}, err
	}
	row := replicaRow{
		ID: in.ID, Revision: in.Revision, Kind: in.Kind, Level: AssetLevelPlatform,
		Name: in.Name, Status: in.Status, Copyable: false, Content: nonempty(in.Content),
		Digest: in.Digest, Deps: deps, ReceivedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			got, getErr := s.ReplicaByIDRev(ctx, in.ID, in.Revision)
			if getErr != nil {
				return AssetReplica{}, getErr
			}
			if !bytes.Equal(got.Digest, in.Digest) {
				return AssetReplica{}, domain.ErrIntegrity
			}
			return got, nil
		}
		return AssetReplica{}, mapAssetWriteErr(err)
	}
	return replicaFromRow(row), nil
}

// ReplicaByIDRev 按身份和修订读平台级副本，含正文。
func (s *Store) ReplicaByIDRev(ctx context.Context, assetID uuid.UUID, revision int64) (AssetReplica, error) {
	var row replicaRow
	if err := s.db.WithContext(ctx).First(&row, "id = ? AND revision = ?", assetID, revision).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AssetReplica{}, domain.ErrNotFound
		}
		return AssetReplica{}, err
	}
	return replicaFromRow(row), nil
}

// LatestReplica 读该身份已收到的最高修订副本。
func (s *Store) LatestReplica(ctx context.Context, assetID uuid.UUID) (AssetReplica, error) {
	var row replicaRow
	if err := s.db.WithContext(ctx).Where("id = ?", assetID).Order("revision DESC").First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AssetReplica{}, domain.ErrNotFound
		}
		return AssetReplica{}, err
	}
	return replicaFromRow(row), nil
}

// ListReplicas 列出本厂已收平台级副本，不含正文。
func (s *Store) ListReplicas(ctx context.Context) ([]AssetReplica, error) {
	var rows []replicaRow
	if err := s.db.WithContext(ctx).Omit("Content").Order("received_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AssetReplica, 0, len(rows))
	for _, row := range rows {
		out = append(out, replicaFromRow(row))
	}
	return out, nil
}

// TamperReplicaContent 只改正文不改摘要，供完整性夹具使用。
func (s *Store) TamperReplicaContent(ctx context.Context, assetID uuid.UUID, revision int64, content []byte) error {
	res := s.db.WithContext(ctx).Model(&replicaRow{}).Where("id = ? AND revision = ?", assetID, revision).Update("content", nonempty(content))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// UpsertClientGrant 授予或重新激活某工程到某 Client。
func (s *Store) UpsertClientGrant(ctx context.Context, projectID, clientID, grantedBy uuid.UUID) (ClientDistributionGrant, error) {
	now := time.Now().UTC()
	var existing clientGrantRow
	err := s.db.WithContext(ctx).First(&existing, "project_id = ? AND client_id = ?", projectID, clientID).Error
	if err == nil {
		existing.Active = true
		existing.GrantedBy = grantedBy
		existing.UpdatedAt = now
		if err := s.db.WithContext(ctx).Save(&existing).Error; err != nil {
			return ClientDistributionGrant{}, err
		}
		return clientGrantFromRow(existing), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return ClientDistributionGrant{}, err
	}
	row := clientGrantRow{
		ID: id.New(), ProjectID: projectID, ClientID: clientID,
		Active: true, GrantedBy: grantedBy, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return ClientDistributionGrant{}, err
	}
	return clientGrantFromRow(row), nil
}

// RevokeClientGrant 收回某工程对某 Client 的下发授权。
func (s *Store) RevokeClientGrant(ctx context.Context, projectID, clientID uuid.UUID) error {
	res := s.db.WithContext(ctx).Model(&clientGrantRow{}).Where("project_id = ? AND client_id = ?", projectID, clientID).Updates(map[string]any{
		"active":     false,
		"updated_at": time.Now().UTC(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ClientGrant 读某工程对某 Client 的授权。
func (s *Store) ClientGrant(ctx context.Context, projectID, clientID uuid.UUID) (ClientDistributionGrant, error) {
	var row clientGrantRow
	if err := s.db.WithContext(ctx).First(&row, "project_id = ? AND client_id = ?", projectID, clientID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ClientDistributionGrant{}, domain.ErrNotFound
		}
		return ClientDistributionGrant{}, err
	}
	return clientGrantFromRow(row), nil
}

// InsertClientRecord 写下发记录；同一工程修订对同一 Client 幂等。
func (s *Store) InsertClientRecord(ctx context.Context, rec ClientDistributionRecord) (ClientDistributionRecord, error) {
	if err := assertAssetDigest(rec.ClosureDigest); err != nil {
		return ClientDistributionRecord{}, err
	}
	members, err := json.Marshal(rec.Members)
	if err != nil {
		return ClientDistributionRecord{}, err
	}
	row := clientRecordRow{
		ID: id.New(), ProjectID: rec.ProjectID, Revision: rec.Revision, ClientID: rec.ClientID,
		ClosureDigest: rec.ClosureDigest, Members: members, CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return s.ClientRecord(ctx, rec.ProjectID, rec.Revision, rec.ClientID)
		}
		return ClientDistributionRecord{}, err
	}
	return clientRecordFromRow(row), nil
}

// ClientRecord 读向某 Client 下发过的一份工程修订。
func (s *Store) ClientRecord(ctx context.Context, projectID uuid.UUID, revision int64, clientID uuid.UUID) (ClientDistributionRecord, error) {
	var row clientRecordRow
	if err := s.db.WithContext(ctx).First(&row, "project_id = ? AND revision = ? AND client_id = ?", projectID, revision, clientID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ClientDistributionRecord{}, domain.ErrNotFound
		}
		return ClientDistributionRecord{}, err
	}
	return clientRecordFromRow(row), nil
}

func replicaFromRow(row replicaRow) AssetReplica {
	return AssetReplica{
		ID: row.ID, Revision: row.Revision, Kind: row.Kind, Level: row.Level, Name: row.Name,
		Status: row.Status, Copyable: row.Copyable, Content: row.Content, Digest: row.Digest,
		Deps: unmarshalAssetDeps(row.Deps), ReceivedAt: row.ReceivedAt,
	}
}

func clientGrantFromRow(row clientGrantRow) ClientDistributionGrant {
	return ClientDistributionGrant{
		ID: row.ID, ProjectID: row.ProjectID, ClientID: row.ClientID, Active: row.Active,
		GrantedBy: row.GrantedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func clientRecordFromRow(row clientRecordRow) ClientDistributionRecord {
	return ClientDistributionRecord{
		ID: row.ID, ProjectID: row.ProjectID, Revision: row.Revision, ClientID: row.ClientID,
		ClosureDigest: row.ClosureDigest, Members: unmarshalAssetDeps(row.Members), CreatedAt: row.CreatedAt,
	}
}
