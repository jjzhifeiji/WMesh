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

// ClosureMember 是闭包里的一条资产快照，含正文。
type ClosureMember struct {
	ID        uuid.UUID  `json:"id"`        // 稳定身份
	Kind      string     `json:"kind"`      // process / project
	Level     string     `json:"level"`     // platform / factory / personal
	Name      string     `json:"name"`      // 显示名
	Status    string     `json:"status"`    // 送达时状态
	Copyable  bool       `json:"copyable"`  // 与源相同
	Revision  int64      `json:"revision"`  // 钉死修订
	Content   []byte     `json:"content"`   // 正文
	Digest    []byte     `json:"digest"`    // 内容 SHA-256
	Deps      []AssetDep `json:"deps"`      // 工艺必须空
	CreatorID *uuid.UUID `json:"creatorId"` // 个人级创建人；其余可空
}

// ClosureSnapshot 是一份工程或单条工艺的完整快照，不是新身份。
type ClosureSnapshot struct {
	Kind            string          `json:"kind"`            // process / project
	AssetID         uuid.UUID       `json:"assetId"`         // 根资产身份
	Revision        int64           `json:"revision"`        // 根修订
	Level           string          `json:"level"`           // 与源相同
	Copyable        bool            `json:"copyable"`        // 与源相同
	Status          string          `json:"status"`          // 与源相同
	TargetFactoryID *uuid.UUID      `json:"targetFactoryId"` // WAN→厂时必填
	TargetClientID  *uuid.UUID      `json:"targetClientId"`  // 厂→Client 或个人级装袋时必填
	Members         []ClosureMember `json:"members"`         // 根在前，其余按 deps 顺序
	Digest          []byte          `json:"digest"`          // 整包 SHA-256
}

// AssetReplica 是已送达本厂的一条平台级（身份, 修订）只读副本。
type AssetReplica struct {
	ID         uuid.UUID  `json:"id"`         // 平台级稳定身份
	Revision   int64      `json:"revision"`   // 送达修订
	Kind       string     `json:"kind"`       // process / project
	Level      string     `json:"level"`      // 固定 platform
	Name       string     `json:"name"`       // 显示名
	Status     string     `json:"status"`     // 送达时状态
	Copyable   bool       `json:"copyable"`   // 与源相同
	Content    []byte     `json:"content"`    // 正文
	Digest     []byte     `json:"digest"`     // SHA-256
	Deps       []AssetDep `json:"deps"`       // 工艺必须空
	ReceivedAt time.Time  `json:"receivedAt"` // 本厂收到时间
	Retracted  bool       `json:"retracted"`  // 云端已删；列表不再展示
}

// FactorySettings 是本厂一份设置，目前只有 Client 工程缓存上限。
type FactorySettings struct {
	ID                int16 `json:"id"`                // 固定 1
	MaxCachedProjects int   `json:"maxCachedProjects"` // 每 Client 工程份上限，≥1
}

// ClientDistributionGrant 是某工程可否下发到某 Client。
type ClientDistributionGrant struct {
	ID        uuid.UUID `json:"id"`        // 授权记录身份
	ProjectID uuid.UUID `json:"projectId"` // 工程身份
	ClientID  uuid.UUID `json:"clientId"`  // 目标 Client
	Active    bool      `json:"active"`    // 是否仍有效
	GrantedBy uuid.UUID `json:"grantedBy"` // 授权超管
	CreatedAt time.Time `json:"createdAt"` // 授权时间
	UpdatedAt time.Time `json:"updatedAt"` // 最近变更
}

// ClientDistributionRecord 是向 Client 下发过的一份工程修订，不含正文。
type ClientDistributionRecord struct {
	ID            uuid.UUID  `json:"id"`            // 记录身份
	ProjectID     uuid.UUID  `json:"projectId"`     // 工程身份
	Revision      int64      `json:"revision"`      // 下发修订
	ClientID      uuid.UUID  `json:"clientId"`      // 目标 Client
	ClosureDigest []byte     `json:"closureDigest"` // 整包摘要
	Members       []AssetDep `json:"members"`       // 成员身份+修订+摘要
	CreatedAt     time.Time  `json:"createdAt"`     // 首次下发时间
}

type replicaRow struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"` // 平台级身份
	Revision   int64     `gorm:"primaryKey"`           // 送达修订
	Kind       string    `gorm:"not null"`             // process / project
	Level      string    `gorm:"not null"`             // platform
	Name       string    `gorm:"not null"`             // 显示名
	Status     string    `gorm:"not null"`             // 送达时状态
	Copyable   bool      `gorm:"not null"`             // 与源相同
	Content    []byte    `gorm:"type:bytea;not null"`  // 正文
	Digest     []byte    `gorm:"type:bytea;not null"`  // SHA-256
	Deps       []byte    `gorm:"type:jsonb;not null"`  // 依赖 JSON
	ReceivedAt time.Time `gorm:"not null"`             // 收到时间
	Retracted  bool      `gorm:"not null"`             // 云端已删；列表不再展示
}

func (replicaRow) TableName() string { return "asset_replicas" }

type factorySettingsRow struct {
	ID                int16 `gorm:"primaryKey"` // 固定 1
	MaxCachedProjects int   `gorm:"not null"`   // 缓存上限
}

func (factorySettingsRow) TableName() string { return "factory_settings" }

type clientGrantRow struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"` // 授权身份
	ProjectID uuid.UUID `gorm:"type:uuid;not null"`   // 工程
	ClientID  uuid.UUID `gorm:"type:uuid;not null"`   // Client
	Active    bool      `gorm:"not null"`             // 是否有效
	GrantedBy uuid.UUID `gorm:"type:uuid;not null"`   // 超管
	CreatedAt time.Time `gorm:"not null"`             // 授权时间
	UpdatedAt time.Time `gorm:"not null"`             // 最近变更
}

func (clientGrantRow) TableName() string { return "client_distribution_grants" }

type clientRecordRow struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"` // 记录身份
	ProjectID     uuid.UUID `gorm:"type:uuid;not null"`   // 工程
	Revision      int64     `gorm:"not null"`             // 修订
	ClientID      uuid.UUID `gorm:"type:uuid;not null"`   // Client
	ClosureDigest []byte    `gorm:"type:bytea;not null"`  // 整包摘要
	Members       []byte    `gorm:"type:jsonb;not null"`  // 成员 JSON
	CreatedAt     time.Time `gorm:"not null"`             // 首次下发
}

func (clientRecordRow) TableName() string { return "client_distribution_records" }

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

// InsertReplica 写入一条平台级副本；同身份修订且摘要相同则原样返回，不重封。
func (s *Store) InsertReplica(ctx context.Context, in AssetReplica) (AssetReplica, error) {
	if err := assertAssetDigest(in.Digest); err != nil {
		return AssetReplica{}, err
	}
	deps, err := marshalAssetDeps(in.Kind, in.Deps)
	if err != nil {
		return AssetReplica{}, err
	}
	got, err := s.ReplicaMetaByIDRev(ctx, in.ID, in.Revision)
	if err == nil {
		if !bytes.Equal(got.Digest, in.Digest) {
			return AssetReplica{}, domain.ErrIntegrity
		}
		return got, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return AssetReplica{}, err
	}
	// 有租约用 MK 重封；无租约拒绝新修订，厂库不落明文。
	env, err := s.persistBody(in.ID, in.Revision, tableReplicas, nonempty(in.Content))
	if err != nil {
		return AssetReplica{}, err
	}
	row := replicaRow{
		ID: in.ID, Revision: in.Revision, Kind: in.Kind, Level: AssetLevelPlatform,
		Name: in.Name, Status: in.Status, Copyable: in.Copyable, Content: env,
		Digest: in.Digest, Deps: deps, ReceivedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			got, getErr := s.ReplicaMetaByIDRev(ctx, in.ID, in.Revision)
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
	return s.decodeReplica(row)
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
	return s.decodeReplica(row)
}

// ReplicaMetaByIDRev 按身份和修订读副本元数据，不解包。
func (s *Store) ReplicaMetaByIDRev(ctx context.Context, assetID uuid.UUID, revision int64) (AssetReplica, error) {
	var row replicaRow
	if err := s.db.WithContext(ctx).Omit("Content").First(&row, "id = ? AND revision = ?", assetID, revision).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AssetReplica{}, domain.ErrNotFound
		}
		return AssetReplica{}, err
	}
	return replicaFromRow(row), nil
}

// LatestReplica 读该身份未撤回的最高修订副本。
func (s *Store) LatestReplica(ctx context.Context, assetID uuid.UUID) (AssetReplica, error) {
	var row replicaRow
	if err := s.db.WithContext(ctx).Where("id = ? AND retracted = ?", assetID, false).Order("revision DESC").First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AssetReplica{}, domain.ErrNotFound
		}
		return AssetReplica{}, err
	}
	return s.decodeReplica(row)
}

// LatestReplicaMeta 读未撤回最高修订副本的元数据，不解包。
func (s *Store) LatestReplicaMeta(ctx context.Context, assetID uuid.UUID) (AssetReplica, error) {
	var row replicaRow
	if err := s.db.WithContext(ctx).Where("id = ? AND retracted = ?", assetID, false).Omit("Content").Order("revision DESC").First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AssetReplica{}, domain.ErrNotFound
		}
		return AssetReplica{}, err
	}
	return replicaFromRow(row), nil
}

// ListReplicas 列出本厂未撤回的平台级副本，不含正文。
func (s *Store) ListReplicas(ctx context.Context) ([]AssetReplica, error) {
	var rows []replicaRow
	if err := s.db.WithContext(ctx).Where("retracted = ?", false).Omit("Content").Order("received_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AssetReplica, 0, len(rows))
	for _, row := range rows {
		out = append(out, replicaFromRow(row))
	}
	return out, nil
}

// RetractReplicas 把该身份全部副本标成撤回；没有副本也算成功。
func (s *Store) RetractReplicas(ctx context.Context, assetID uuid.UUID) error {
	return s.db.WithContext(ctx).Model(&replicaRow{}).Where("id = ?", assetID).Update("retracted", true).Error
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
		Deps: unmarshalAssetDeps(row.Deps), ReceivedAt: row.ReceivedAt, Retracted: row.Retracted,
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
