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
	// 审计记下用的是哪一口钟
	src := bagTimeSource(*bag)
	// 他厂袋直接拒绝，不发凭证
	if err := s.assertBagFactory(*bag); err != nil {
		// 记下事实入队被拒绝，写失败不改变结果
		_ = s.auditTimed(ctx, personActor(bag), &loginName, "enqueue_fact", bag.ClientID.String(), audit.Deny, src)
		return PendingFact{}, err
	}
	// 账号、凭证、资产桩都过才允许
	p, ev := s.evalOffline(ctx, *bag, clocks, loginName, password)
	// 判定不是允许则拒绝操作或改记审计
	if ev.Decision != NodeAllow {
		// 记下事实入队被拒绝，写失败不改变结果
		_ = s.auditTimed(ctx, personActor(bag), &loginName, "enqueue_fact", bag.ClientID.String(), audit.Deny, src)
		return PendingFact{}, domain.ErrForbidden
	}
	// 收成不含口令的对外账号
	unitID, path, err := s.resolveWorkContext(ctx, accountOf(p), wc)
	// 账号视图收不成就不交回
	if err != nil {
		// 记下事实入队被拒绝，写失败不改变结果
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
	// 把这一条收进结果，漏了清单就不齐
	bag.PendingFacts = append(bag.PendingFacts, item)
	// 事实入队成功后记审计，再把结果交回
	return item, s.auditTimed(ctx, &p.ID, &loginName, "enqueue_fact", item.ID.String(), audit.Allow, src)
}

// EnqueueUpload 把点云或图片放进本机待发，正文不进厂库。
func (s *Sync) EnqueueUpload(ctx context.Context, bag *Bag, clocks Clocks, loginName, password, kind string, content []byte) (PendingUpload, error) {
	// 审计记下用的是哪一口钟
	src := bagTimeSource(*bag)
	// 他厂袋直接拒绝，不发凭证
	if err := s.assertBagFactory(*bag); err != nil {
		// 记下上传入队被拒绝，写失败不改变结果
		_ = s.auditTimed(ctx, personActor(bag), &loginName, "enqueue_upload", bag.ClientID.String(), audit.Deny, src)
		return PendingUpload{}, err
	}
	// 不是点云也不是图片则拒绝入队
	if kind != UploadPointCloud && kind != UploadImage {
		// 记下上传入队被拒绝，写失败不改变结果
		_ = s.auditTimed(ctx, personActor(bag), &loginName, "enqueue_upload", kind, audit.Deny, src)
		return PendingUpload{}, domain.ErrForbidden
	}
	// 账号、凭证、资产桩都过才允许
	p, ev := s.evalOffline(ctx, *bag, clocks, loginName, password)
	// 判定不是允许则拒绝操作或改记审计
	if ev.Decision != NodeAllow {
		// 记下上传入队被拒绝，写失败不改变结果
		_ = s.auditTimed(ctx, personActor(bag), &loginName, "enqueue_upload", bag.ClientID.String(), audit.Deny, src)
		return PendingUpload{}, domain.ErrForbidden
	}
	// 复制一份字节，避免和原来的钥或正文共用底层
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
	// 把这一条收进结果，漏了清单就不齐
	bag.PendingUploads = append(bag.PendingUploads, item)
	// 上传入队成功后记审计，再把结果交回
	return item, s.auditTimed(ctx, &p.ID, &loginName, "enqueue_upload", item.ID.String(), audit.Allow, src)
}

// ConvergeIntent 已连网拉取本厂最新节点运行凭证，只接受更高修订。
func (s *Sync) ConvergeIntent(ctx context.Context, bag *Bag) error {
	// 审计记下用的是哪一口钟
	src := bagTimeSource(*bag)
	// 不在线则拒绝汇聚
	if err := s.assertOnlineBag(ctx, *bag); err != nil {
		// 记下收敛意图被拒绝，写失败不改变结果
		_ = s.auditTimed(ctx, personActor(bag), nil, "converge_intent", bag.ClientID.String(), audit.Deny, src)
		return err
	}
	// 只接受更高修订。
	rt, err := s.store.LatestRuntimeGrant(ctx, bag.ClientID)
	// 不是没有记录，读取真失败必须停住
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		// 记录不存在，记下收敛意图被拒绝，写失败不改变结果
		_ = s.auditTimed(ctx, personActor(bag), nil, "converge_intent", bag.ClientID.String(), audit.Deny, src)
		return err
	}
	// 没有错误才采用这次结果，失败另走拒绝
	if err == nil {
		// 从库里的行还原运行凭证
		cred, decErr := runtimeFromRow(rt)
		// 声明还原失败则不用这张凭证往下收敛
		if decErr != nil {
			// 记下收敛意图被拒绝，写失败不改变结果
			_ = s.auditTimed(ctx, personActor(bag), nil, "converge_intent", bag.ClientID.String(), audit.Deny, src)
			return domain.ErrInvalidKey
		}
		// 只接受更高修订且身份匹配的凭证
		bag.ApplyRuntime(cred)
	}
	// 收敛意图成功后记审计，再把结果交回
	return s.auditTimed(ctx, personActor(bag), nil, "converge_intent", bag.ClientID.String(), audit.Allow, src)
}

// FlushPending 把待发汇进本厂；失败则队列保留。
func (s *Sync) FlushPending(ctx context.Context, bag *Bag) error {
	// 审计记下用的是哪一口钟
	src := bagTimeSource(*bag)
	// 收成文本身份，供审计或对账对准
	target := bag.ClientID.String()
	// 不在线则拒绝汇聚
	if err := s.assertOnlineBag(ctx, *bag); err != nil {
		// 记下汇聚待发被拒绝，写失败不改变结果
		_ = s.auditTimed(ctx, personActor(bag), nil, "sync_flush", target, audit.Deny, src)
		return err
	}
	// 这次被指定汇聚失败，队列保留可再试
	if bag.FailFlush {
		// 这次汇聚被指定失败，记下汇聚待发被拒绝，写失败不改变结果
		_ = s.auditTimed(ctx, personActor(bag), nil, "sync_flush", target, audit.Deny, src)
		return domain.ErrSyncRetry
	}
	// 队列还有就继续汇聚，失败则留下次再试
	for len(bag.PendingFacts) > 0 {
		// 取出队列头一条事实，写成功才弹出
		item := bag.PendingFacts[0]
		// 按产生端身份幂等写入。
		if _, err := s.store.MergeFact(ctx, FactStub{
			// 按产生端身份幂等写入，路径用当时快照
			ID: item.ID, CreatorID: item.CreatorID, OrgUnitID: item.OrgUnitID, OrgPath: item.OrgPath,
		}); err != nil {
			_ = s.auditTimed(ctx, personActor(bag), nil, "sync_flush", item.ID.String(), audit.Deny, src)
			return err
		}
		// 这条已写入厂库，从待发队列拿掉
		bag.PendingFacts = bag.PendingFacts[1:]
	}
	// 队列还有就继续汇聚，失败则留下次再试
	for len(bag.PendingUploads) > 0 {
		// 取出队列头一条上传，写成功才弹出
		item := bag.PendingUploads[0]
		// 写入失败则这条留在待发队列
		if err := s.mergeOneUpload(ctx, *bag, item); err != nil {
			// 记下汇聚待发被拒绝，写失败不改变结果
			_ = s.auditTimed(ctx, personActor(bag), nil, "sync_flush", item.ID.String(), audit.Deny, src)
			return err
		}
		// 这条已汇聚，从待发队列拿掉
		bag.PendingUploads = bag.PendingUploads[1:]
	}
	// 汇聚待发成功后记审计，再把结果交回
	return s.auditTimed(ctx, personActor(bag), nil, "sync_flush", target, audit.Allow, src)
}

// Reconnect 回连：先收敛意图，再送待发。
func (s *Sync) Reconnect(ctx context.Context, bag *Bag) error {
	// 意图对不上则不送待发
	if err := s.ConvergeIntent(ctx, bag); err != nil {
		return err
	}
	// 把本机待发写入厂库，失败则队列保留，再交回调用方
	return s.FlushPending(ctx, bag)
}

// GetUpload 只读上传记录，不含正文。
func (s *Sync) GetUpload(ctx context.Context, token string, uploadID uuid.UUID) (UploadRecord, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return UploadRecord{}, err
	}
	// 只读元数据，不含正文。
	row, err := s.store.UploadByID(ctx, uploadID)
	// 没有这条上传则拒绝
	if err != nil {
		return UploadRecord{}, err
	}
	// 既不是创建人又无只读许可，拒绝查看
	if err := s.canReadMeta(ctx, acc, row.CreatorID, nil); err != nil {
		// 记下读取上传被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "get_upload", uploadID.String(), audit.Deny)
		return UploadRecord{}, err
	}
	// 读取上传成功后记审计，再把结果交回
	return row, s.audit(ctx, &acc.ID, nil, "get_upload", uploadID.String(), audit.Allow)
}

// ReadUploadContent 取出上传正文；不把正文写入审计。
func (s *Sync) ReadUploadContent(ctx context.Context, token string, uploadID uuid.UUID) ([]byte, error) {
	// 读一条上传记录，避免留下不一致的结果
	row, err := s.GetUpload(ctx, token, uploadID)
	// 记录读失败则不能汇聚
	if err != nil {
		return nil, err
	}
	// 正文在对象存储，不进审计。
	body, err := s.blobs.Get(ctx, row.ObjectKey)
	// 取不到则拒绝阅读
	if err != nil {
		// 命中这种预期错误
		if errors.Is(err, blob.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return body, nil
}

// mergeOneUpload 摘要一致则幂等跳过；否则写入对象存储并落元数据。
func (s *Sync) mergeOneUpload(ctx context.Context, bag Bag, item PendingUpload) error {
	// 条件不成立则拒绝或跳过，不往下改数据
	if !digest.Match(item.Content, item.Digest) {
		return domain.ErrIntegrity
	}
	// 取出本厂稳定身份，封包和审计都要用
	key := s.store.FactoryID().String() + "/" + item.Kind + "/" + item.ID.String()
	// 按身份读上传记录
	existing, err := s.store.UploadByID(ctx, item.ID)
	// 没有错误才采用这次结果，失败另走拒绝
	if err == nil {
		// 不是创建人则个人级拒绝，或改看授权
		if existing.Kind != item.Kind || !bytes.Equal(existing.Digest, item.Digest) || existing.CreatorID != item.CreatorID || existing.ClientID != item.ClientID {
			return domain.ErrIntegrity
		}
		return nil
	}
	// 不是没有记录，读取真失败必须停住
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	// 正文进对象存储，库只落元数据和摘要。
	if err := s.blobs.Put(ctx, key, item.Content); err != nil {
		return err
	}
	// 汇聚一条上传，失败可再试
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
	// 袋不是这台或不是本厂，直接拒绝
	if bag.FactoryID != s.store.FactoryID() {
		return domain.ErrForbidden
	}
	return nil
}

// assertOnlineBag 须连网且 Client 仍绑定本厂。
func (s *Sync) assertOnlineBag(ctx context.Context, bag Bag) error {
	// 他厂袋直接拒绝，不发凭证
	if err := s.assertBagFactory(bag); err != nil {
		return err
	}
	// 连着厂服就改用服务器钟
	if !bag.Connected {
		return domain.ErrForbidden
	}
	// 按身份读本厂这台设备
	cl, err := s.store.ClientByID(ctx, bag.ClientID)
	// 这台设备不在名录，拒绝
	if err != nil {
		return err
	}
	// 不是有效绑定则拒绝签发、登录或对账
	if cl.Status != ClientStatusBound {
		return domain.ErrBindingVoid
	}
	return nil
}

// runtimeFromRow 从库行还原已签名凭证。
func runtimeFromRow(row RuntimeGrant) (RuntimeCred, error) {
	// 从声明原文还原凭证字段
	cred, err := decodeRuntime(row.Payload)
	// 声明还原失败，不当作有效凭证
	if err != nil {
		return RuntimeCred{}, err
	}
	// 带上签名，对不上就当没持有这张凭证
	cred.Signature = row.Signature
	return cred, nil
}
