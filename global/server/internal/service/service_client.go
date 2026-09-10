package service

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/domain"
)

// BindClient 由 WAN 管理员登记公钥并把未绑定节点绑到一厂。
func (s *Service) BindClient(ctx context.Context, token string, clientID, factoryID uuid.UUID, publicKey []byte) (Client, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	if _, err := s.store.FactoryByID(ctx, factoryID); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	c, err := s.store.ClientByID(ctx, clientID)
	if errors.Is(err, domain.ErrNotFound) {
		c, err = s.store.CreateClient(ctx, clientID, publicKey)
		if err != nil {
			_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
			return Client{}, err
		}
	} else if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	} else if !bytes.Equal(c.PublicKey, publicKey) {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, domain.ErrInvalidKey
	}
	bound, err := s.store.BindClient(ctx, c.ID, factoryID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "bind_client", clientID.String()+" "+factoryID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	return bound, nil
}

// RebindClient 把已绑定节点改到另一厂，绑定修订升高；旧厂不再是当前所属。
func (s *Service) RebindClient(ctx context.Context, token string, clientID, factoryID uuid.UUID) (Client, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, &factoryID, "rebind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	bound, err := s.store.RebindClient(ctx, clientID, factoryID)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, &factoryID, "rebind_client", clientID.String(), audit.Deny)
		return Client{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, &factoryID, "rebind_client", clientID.String()+" "+factoryID.String(), audit.Allow); err != nil {
		return Client{}, err
	}
	return bound, nil
}

func (s *Service) ClientByID(ctx context.Context, clientID uuid.UUID) (Client, error) {
	return s.store.ClientByID(ctx, clientID)
}

// ListPersonOfflineGrants 从 WAN 查人员离线授权，一律拒绝。
func (s *Service) ListPersonOfflineGrants(ctx context.Context, token string, factoryID uuid.UUID) error {
	return s.denyFactoryManage(ctx, token, factoryID, "list_person_offline_grants", factoryID.String())
}
