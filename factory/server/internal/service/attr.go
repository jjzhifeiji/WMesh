package service

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
)

// personOrgSnap 取这个人现在的所属组织和路径；没有所属组织则厂直属。
func (s *kernel) personOrgSnap(ctx context.Context, personID uuid.UUID) (*uuid.UUID, []PathNode, error) {
	// 按稳定身份读人员，所属组织挂在人员行上
	p, err := s.store.PersonByID(ctx, personID)
	// 人员读不到，拿不到组织
	if err != nil {
		return nil, nil, err
	}
	// 没有所属组织就是厂直属，路径为空
	if p.OrgUnitID == nil {
		return nil, []PathNode{}, nil
	}
	// 按当前树走出路径
	path, err := s.store.PathSnapshot(ctx, *p.OrgUnitID)
	// 路径走不出来，不交残缺路径
	if err != nil {
		return nil, nil, err
	}
	return p.OrgUnitID, path, nil
}

// resolveWorkContext 产生事实前核操作权，组织取这个人现在的所属组织。
func (s *kernel) resolveWorkContext(ctx context.Context, acc Account) (*uuid.UUID, []PathNode, error) {
	// 没有操作权就不能产生事实
	if err := s.can(ctx, acc, permOperate); err != nil {
		return nil, []PathNode{}, err
	}
	// 组织跟人走，取此人现在的所属组织
	return s.personOrgSnap(ctx, acc.ID)
}

// canReadMeta 创建人或有报表只读权的人才能看元数据。
func (s *kernel) canReadMeta(ctx context.Context, acc Account, creatorID uuid.UUID) error {
	// 创建人可以看自己的
	if acc.ID == creatorID {
		return nil
	}
	// 其余看只读权
	return s.can(ctx, acc, permView)
}

// 用服务器时间记下允许或拒绝。
func (s *kernel) auditAt(ctx context.Context, actor *uuid.UUID, action, target, result string) error {
	// 改用服务器钟，把允许或拒绝记下
	return s.auditAtSrc(ctx, actor, action, target, result, audit.Server, nil, nil)
}

// auditAtSrc 记下带组织路径和时间来源的允许或拒绝。
func (s *kernel) auditAtSrc(ctx context.Context, actor *uuid.UUID, action, target, result, timeSource string, unit *uuid.UUID, path []PathNode) error {
	// 取出本厂稳定身份，封包和审计都要用
	fid := s.store.FactoryID()
	// 路径为空就不带快照正文，避免写下空对象
	var raw []byte
	// 有路径快照才写入审计，没有则只记动作
	if path != nil {
		// 收成稳定正文，避免留下不一致的结果
		raw, _ = json.Marshal(path)
	}
	// 把允许或拒绝写入审计，不含口令
	return s.store.AppendAudit(ctx, audit.Event{
		ActorID:    actor,
		FactoryID:  &fid,
		OrgUnitID:  unit,
		OrgPath:    raw,
		Action:     action,
		Target:     target,
		Result:     result,
		TimeSource: timeSource,
	})
}
