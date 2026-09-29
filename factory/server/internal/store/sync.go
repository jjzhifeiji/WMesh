package store

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/factory/internal/platform/domain"
)

const (
	UploadPointCloud = "point_cloud" // 点云
	UploadImage      = "image"       // 图片
)

// UploadRecord 是 Client 主动上传点云/图片的元数据，不含正文。
type UploadRecord struct {
	ID        uuid.UUID // 产生端稳定身份，汇聚幂等键
	Kind      string    // point_cloud / image
	ObjectKey string    // 对象存储键
	Digest    []byte    // SHA-256 摘要 32 字节
	ByteSize  int64     // 正文字节数
	CreatorID uuid.UUID // 上传人
	ClientID  uuid.UUID // 来源 Client
	CreatedAt time.Time // 首次汇聚时间
}

// 上传记录的落库行，只存元数据。
type uploadRow struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"` // 产生端稳定身份
	Kind      string    `gorm:"not null"`             // point_cloud / image
	ObjectKey string    `gorm:"not null"`             // 对象键
	Digest    []byte    `gorm:"type:bytea;not null"`  // SHA-256
	ByteSize  int64     `gorm:"not null"`             // 字节数
	CreatorID uuid.UUID `gorm:"type:uuid;not null"`   // 上传人
	ClientID  uuid.UUID `gorm:"type:uuid;not null"`   // 来源 Client
	CreatedAt time.Time `gorm:"not null"`             // 首次汇聚时间
}

// 指定落库表名，避免查询时按类型名去猜。
func (uploadRow) TableName() string { return "upload_records" }

// MergeUpload 按产生端身份写入上传记录；同摘要原样返回，不同则拒绝覆盖。
func (s *Store) MergeUpload(ctx context.Context, in UploadRecord) (UploadRecord, error) {
	// 没有身份就不能当有效目标。
	if in.ID == uuid.Nil {
		return UploadRecord{}, domain.ErrNotFound
	}
	// 先按身份读上传记录，结果留给紧跟着的判断。
	got, err := s.UploadByID(ctx, in.ID)
	// 已经有记录就核对摘要，不一致不能覆盖。
	if err == nil {
		// 已有行摘要或来源不同不能覆盖。
		if got.Kind != in.Kind || !bytes.Equal(got.Digest, in.Digest) || got.CreatorID != in.CreatorID || got.ClientID != in.ClientID {
			return UploadRecord{}, domain.ErrIntegrity
		}
		return got, nil
	}
	// 不是找不到的错误要原样交回。
	if !errors.Is(err, domain.ErrNotFound) {
		return UploadRecord{}, err
	}
	// 组装要落库的行，正文或元数据按入参填。
	row := uploadRow{
		ID:        in.ID,
		Kind:      in.Kind,
		ObjectKey: in.ObjectKey,
		Digest:    in.Digest,
		ByteSize:  in.ByteSize,
		CreatorID: in.CreatorID,
		ClientID:  in.ClientID,
		CreatedAt: time.Now().UTC(),
	}
	// 写入一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 唯一约束撞了就改成业务冲突，不抛库原文。
		if domain.IsUniqueViolation(err) {
			// 做完汇聚上传记录后把结果交回。
			return s.MergeUpload(ctx, in)
		}
		// 外键对不上就当被指的对象不存在。
		if domain.IsForeignKeyViolation(err) {
			return UploadRecord{}, domain.ErrNotFound
		}
		// 检查约束不通过就改成业务拒绝。
		if domain.IsCheckViolation(err) {
			return UploadRecord{}, domain.ErrForbidden
		}
		return UploadRecord{}, err
	}
	// 库行收成对外结果再交回。
	return uploadFromRow(row), nil
}

// UploadByID 读一条上传记录，不含正文。
func (s *Store) UploadByID(ctx context.Context, uploadID uuid.UUID) (UploadRecord, error) {
	// 准备承接查到的上传记录。
	var row uploadRow
	// 按条件取一行失败就停，避免带着错误继续。
	if err := s.db.WithContext(ctx).First(&row, "id = ?", uploadID).Error; err != nil {
		// 没有这一行就当成不存在。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return UploadRecord{}, domain.ErrNotFound
		}
		return UploadRecord{}, err
	}
	// 库行收成对外结果再交回。
	return uploadFromRow(row), nil
}

// 库行收成上传元数据，不含正文。
func uploadFromRow(row uploadRow) UploadRecord {
	return UploadRecord{
		ID:        row.ID,
		Kind:      row.Kind,
		ObjectKey: row.ObjectKey,
		Digest:    row.Digest,
		ByteSize:  row.ByteSize,
		CreatorID: row.CreatorID,
		ClientID:  row.ClientID,
		CreatedAt: row.CreatedAt,
	}
}
