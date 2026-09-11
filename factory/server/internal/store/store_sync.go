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

// MergeUpload 按产生端身份写入上传记录；同摘要原样返回，不同则拒绝覆盖。
func (s *Store) MergeUpload(ctx context.Context, in UploadRecord) (UploadRecord, error) {
	if in.ID == uuid.Nil {
		return UploadRecord{}, domain.ErrNotFound
	}
	got, err := s.UploadByID(ctx, in.ID)
	if err == nil {
		if got.Kind != in.Kind || !bytes.Equal(got.Digest, in.Digest) || got.CreatorID != in.CreatorID || got.ClientID != in.ClientID {
			return UploadRecord{}, domain.ErrIntegrity
		}
		return got, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return UploadRecord{}, err
	}
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
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return s.MergeUpload(ctx, in)
		}
		if domain.IsForeignKeyViolation(err) {
			return UploadRecord{}, domain.ErrNotFound
		}
		if domain.IsCheckViolation(err) {
			return UploadRecord{}, domain.ErrForbidden
		}
		return UploadRecord{}, err
	}
	return uploadFromRow(row), nil
}

// UploadByID 读一条上传记录，不含正文。
func (s *Store) UploadByID(ctx context.Context, uploadID uuid.UUID) (UploadRecord, error) {
	var row uploadRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", uploadID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return UploadRecord{}, domain.ErrNotFound
		}
		return UploadRecord{}, err
	}
	return uploadFromRow(row), nil
}

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
