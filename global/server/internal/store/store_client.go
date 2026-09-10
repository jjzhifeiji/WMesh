package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/platform/id"
)

// PutFactoryPublicKey 登记该厂签发公钥；一厂一把，不存私钥。
func (s *Store) PutFactoryPublicKey(ctx context.Context, factoryID uuid.UUID, publicKey []byte) error {
	row := FactoryPublicKey{
		FactoryID: factoryID,
		PublicKey: publicKey,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return domain.ErrFactoryKeyExists
		}
		if domain.IsCheckViolation(err) {
			return domain.ErrInvalidKey
		}
		if domain.IsForeignKeyViolation(err) {
			return domain.ErrNotFound
		}
		return err
	}
	return nil
}

func (s *Store) FactoryPublicKey(ctx context.Context, factoryID uuid.UUID) (FactoryPublicKey, error) {
	var row FactoryPublicKey
	if err := s.db.WithContext(ctx).First(&row, "factory_id = ?", factoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FactoryPublicKey{}, domain.ErrNotFound
		}
		return FactoryPublicKey{}, err
	}
	return row, nil
}

// CreateClient 登记一台未绑定 Client 的公钥；身份由调用方给出或现场发号。
func (s *Store) CreateClient(ctx context.Context, clientID uuid.UUID, publicKey []byte) (Client, error) {
	if clientID == uuid.Nil {
		clientID = id.New()
	}
	row := Client{
		ID:              clientID,
		PublicKey:       publicKey,
		BindingRevision: 0,
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if domain.IsUniqueViolation(err) {
			return Client{}, domain.ErrClientKeyTaken
		}
		if domain.IsCheckViolation(err) {
			return Client{}, domain.ErrInvalidKey
		}
		return Client{}, err
	}
	return row, nil
}

func (s *Store) ClientByID(ctx context.Context, clientID uuid.UUID) (Client, error) {
	var row Client
	if err := s.db.WithContext(ctx).First(&row, "id = ?", clientID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Client{}, domain.ErrNotFound
		}
		return Client{}, err
	}
	return row, nil
}

// ListClients 列出已登记的现场节点，不含私钥。
func (s *Store) ListClients(ctx context.Context) ([]Client, error) {
	var rows []Client
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []Client{}
	}
	return rows, nil
}

// BindClient 把未绑定 Client 绑到一厂，绑定修订从 0 升到 1。
func (s *Store) BindClient(ctx context.Context, clientID, factoryID uuid.UUID) (Client, error) {
	now := time.Now().UTC()
	var out Client
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Client
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		if row.FactoryID != nil {
			return domain.ErrClientBound
		}
		if err := tx.First(&Factory{}, "id = ?", factoryID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		res := tx.Model(&Client{}).Where("id = ? AND factory_id IS NULL", clientID).Updates(map[string]any{
			"factory_id":       factoryID,
			"binding_revision": int64(1),
			"bound_at":         now,
		})
		if res.Error != nil {
			if domain.IsCheckViolation(res.Error) {
				return domain.ErrClientBound
			}
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrClientBound
		}
		row.FactoryID = &factoryID
		row.BindingRevision = 1
		row.BoundAt = &now
		out = row
		return nil
	})
	return out, err
}

// RebindClient 把已绑定 Client 改到另一厂，绑定修订必须升高。
func (s *Store) RebindClient(ctx context.Context, clientID, factoryID uuid.UUID) (Client, error) {
	now := time.Now().UTC()
	var out Client
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Client
		if err := tx.First(&row, "id = ?", clientID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		if row.FactoryID == nil {
			return domain.ErrUnbound
		}
		if *row.FactoryID == factoryID {
			return domain.ErrClientBound
		}
		if err := tx.First(&Factory{}, "id = ?", factoryID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrNotFound
			}
			return err
		}
		next := row.BindingRevision + 1
		res := tx.Model(&Client{}).Where("id = ? AND factory_id IS NOT NULL AND factory_id <> ?", clientID, factoryID).
			Updates(map[string]any{
				"factory_id":       factoryID,
				"binding_revision": next,
				"bound_at":         now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return domain.ErrClientBound
		}
		row.FactoryID = &factoryID
		row.BindingRevision = next
		row.BoundAt = &now
		out = row
		return nil
	})
	return out, err
}

func (s *Store) HasColumn(ctx context.Context, table, column string) (bool, error) {
	var exists bool
	err := s.db.WithContext(ctx).
		Raw("SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? AND column_name = ?)", table, column).
		Scan(&exists).Error
	return exists, err
}
