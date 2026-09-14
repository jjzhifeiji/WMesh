package service

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/blob"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/id"
)

// EnqueueFact 把运行事实放进本机待发，不写厂库。
func (s *Sync) EnqueueFact(ctx context.Context, bag *Bag, clocks Clocks, loginName, password string, wc WorkContext) (PendingFact, error) {
	src := bagTimeSource(*bag)
	if err := s.assertBagFactory(*bag); err != nil {
		_ = s.auditTimed(ctx, personActor(bag), &loginName, "enqueue_fact", bag.ClientID.String(), audit.Deny, src)
		return PendingFact{}, err
	}
	p, ev := s.evalOffline(ctx, *bag, clocks, loginName, password)
	if ev.Decision != NodeAllow {
		_ = s.auditTimed(ctx, personActor(bag), &loginName, "enqueue_fact", bag.ClientID.String(), audit.Deny, src)
		return PendingFact{}, domain.ErrForbidden
	}
	unitID, path, err := s.resolveWorkContext(ctx, accountOf(p), wc)
	if err != nil {
		_ = s.auditTimed(ctx, &p.ID, &loginName, "enqueue_fact", bag.ClientID.String(), audit.Deny, src)
		return PendingFact{}, err
	}
	// 产生端发号，汇聚时当幂等键。
	item := PendingFact{
		ID:        id.New(),
		CreatorID: p.ID,
		OrgUnitID: unitID,
		OrgPath:   append([]PathNode(nil), path...),
	}
	bag.PendingFacts = append(bag.PendingFacts, item)
	return item, s.auditTimed(ctx, &p.ID, &loginName, "enqueue_fact", item.ID.String(), audit.Allow, src)
}

// EnqueueUpload 把点云或图片放进本机待发，正文不进厂库。
func (s *Sync) EnqueueUpload(ctx context.Context, bag *Bag, clocks Clocks, loginName, password, kind string, content []byte) (PendingUpload, error) {
	src := bagTimeSource(*bag)
	if err := s.assertBagFactory(*bag); err != nil {
		_ = s.auditTimed(ctx, personActor(bag), &loginName, "enqueue_upload", bag.ClientID.String(), audit.Deny, src)
		return PendingUpload{}, err
	}
	if kind != UploadPointCloud && kind != UploadImage {
		_ = s.auditTimed(ctx, personActor(bag), &loginName, "enqueue_upload", kind, audit.Deny, src)
		return PendingUpload{}, domain.ErrForbidden
	}
	p, ev := s.evalOffline(ctx, *bag, clocks, loginName, password)
	if ev.Decision != NodeAllow {
		_ = s.auditTimed(ctx, personActor(bag), &loginName, "enqueue_upload", bag.ClientID.String(), audit.Deny, src)
		return PendingUpload{}, domain.ErrForbidden
	}
	body := append([]byte(nil), content...)
	// 产生端发号；正文只在袋内，摘要一并记下。
	item := PendingUpload{
		ID:        id.New(),
		Kind:      kind,
		Content:   body,
		Digest:    digest.Sum(body),
		CreatorID: p.ID,
		ClientID:  bag.ClientID,
	}
	bag.PendingUploads = append(bag.PendingUploads, item)
	return item, s.auditTimed(ctx, &p.ID, &loginName, "enqueue_upload", item.ID.String(), audit.Allow, src)
}

// ConvergeIntent 已连网拉取本厂最新节点运行凭证，只接受更高修订。
func (s *Sync) ConvergeIntent(ctx context.Context, bag *Bag) error {
	src := bagTimeSource(*bag)
	if err := s.assertOnlineBag(ctx, *bag); err != nil {
		_ = s.auditTimed(ctx, personActor(bag), nil, "converge_intent", bag.ClientID.String(), audit.Deny, src)
		return err
	}
	// 只接受更高修订。
	rt, err := s.store.LatestRuntimeGrant(ctx, bag.ClientID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		_ = s.auditTimed(ctx, personActor(bag), nil, "converge_intent", bag.ClientID.String(), audit.Deny, src)
		return err
	}
	if err == nil {
		cred, decErr := runtimeFromRow(rt)
		if decErr != nil {
			_ = s.auditTimed(ctx, personActor(bag), nil, "converge_intent", bag.ClientID.String(), audit.Deny, src)
			return domain.ErrInvalidKey
		}
		bag.ApplyRuntime(cred)
	}
	return s.auditTimed(ctx, personActor(bag), nil, "converge_intent", bag.ClientID.String(), audit.Allow, src)
}

// FlushPending 把待发汇进本厂；失败则队列保留。
func (s *Sync) FlushPending(ctx context.Context, bag *Bag) error {
	src := bagTimeSource(*bag)
	target := bag.ClientID.String()
	if err := s.assertOnlineBag(ctx, *bag); err != nil {
		_ = s.auditTimed(ctx, personActor(bag), nil, "sync_flush", target, audit.Deny, src)
		return err
	}
	if bag.FailFlush {
		_ = s.auditTimed(ctx, personActor(bag), nil, "sync_flush", target, audit.Deny, src)
		return domain.ErrSyncRetry
	}
	for len(bag.PendingFacts) > 0 {
		item := bag.PendingFacts[0]
		// 按产生端身份幂等写入。
		if _, err := s.store.MergeFact(ctx, FactStub{
			ID: item.ID, CreatorID: item.CreatorID, OrgUnitID: item.OrgUnitID, OrgPath: item.OrgPath,
		}); err != nil {
			_ = s.auditTimed(ctx, personActor(bag), nil, "sync_flush", item.ID.String(), audit.Deny, src)
			return err
		}
		bag.PendingFacts = bag.PendingFacts[1:]
	}
	for len(bag.PendingUploads) > 0 {
		item := bag.PendingUploads[0]
		if err := s.mergeOneUpload(ctx, *bag, item); err != nil {
			_ = s.auditTimed(ctx, personActor(bag), nil, "sync_flush", item.ID.String(), audit.Deny, src)
			return err
		}
		bag.PendingUploads = bag.PendingUploads[1:]
	}
	return s.auditTimed(ctx, personActor(bag), nil, "sync_flush", target, audit.Allow, src)
}

// Reconnect 回连：先收敛意图，再送待发。
func (s *Sync) Reconnect(ctx context.Context, bag *Bag) error {
	if err := s.ConvergeIntent(ctx, bag); err != nil {
		return err
	}
	return s.FlushPending(ctx, bag)
}

// GetUpload 只读上传记录，不含正文。
func (s *Sync) GetUpload(ctx context.Context, token string, uploadID uuid.UUID) (UploadRecord, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return UploadRecord{}, err
	}
	// 只读元数据，不含正文。
	row, err := s.store.UploadByID(ctx, uploadID)
	if err != nil {
		return UploadRecord{}, err
	}
	if err := s.canReadMeta(ctx, acc, row.CreatorID, nil); err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_upload", uploadID.String(), audit.Deny)
		return UploadRecord{}, err
	}
	return row, s.audit(ctx, &acc.ID, nil, "get_upload", uploadID.String(), audit.Allow)
}

// ReadUploadContent 取出上传正文；不把正文写入审计。
func (s *Sync) ReadUploadContent(ctx context.Context, token string, uploadID uuid.UUID) ([]byte, error) {
	row, err := s.GetUpload(ctx, token, uploadID)
	if err != nil {
		return nil, err
	}
	// 正文在对象存储，不进审计。
	body, err := s.blobs.Get(ctx, row.ObjectKey)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return body, nil
}

// mergeOneUpload 摘要一致则幂等跳过；否则写入对象存储并落元数据。
func (s *Sync) mergeOneUpload(ctx context.Context, bag Bag, item PendingUpload) error {
	if !digest.Match(item.Content, item.Digest) {
		return domain.ErrIntegrity
	}
	key := s.store.FactoryID().String() + "/" + item.Kind + "/" + item.ID.String()
	existing, err := s.store.UploadByID(ctx, item.ID)
	if err == nil {
		if existing.Kind != item.Kind || !bytes.Equal(existing.Digest, item.Digest) || existing.CreatorID != item.CreatorID || existing.ClientID != item.ClientID {
			return domain.ErrIntegrity
		}
		return nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	// 正文进对象存储，库只落元数据和摘要。
	if err := s.blobs.Put(ctx, key, item.Content); err != nil {
		return err
	}
	_, err = s.store.MergeUpload(ctx, UploadRecord{
		ID:        item.ID,
		Kind:      item.Kind,
		ObjectKey: key,
		Digest:    item.Digest,
		ByteSize:  int64(len(item.Content)),
		CreatorID: item.CreatorID,
		ClientID:  item.ClientID,
	})
	return err
}

// assertBagFactory 袋必须认定本厂。
func (s *Sync) assertBagFactory(bag Bag) error {
	if bag.FactoryID != s.store.FactoryID() {
		return domain.ErrForbidden
	}
	return nil
}

// assertOnlineBag 须连网且 Client 仍绑定本厂。
func (s *Sync) assertOnlineBag(ctx context.Context, bag Bag) error {
	if err := s.assertBagFactory(bag); err != nil {
		return err
	}
	if !bag.Connected {
		return domain.ErrForbidden
	}
	cl, err := s.store.ClientByID(ctx, bag.ClientID)
	if err != nil {
		return err
	}
	if cl.Status != ClientStatusBound {
		return domain.ErrBindingVoid
	}
	return nil
}

// runtimeFromRow 从库行还原已签名凭证。
func runtimeFromRow(row RuntimeGrant) (RuntimeCred, error) {
	cred, err := decodeRuntime(row.Payload)
	if err != nil {
		return RuntimeCred{}, err
	}
	cred.Signature = row.Signature
	return cred, nil
}
