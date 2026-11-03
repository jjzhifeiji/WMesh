package store

import (
	"context"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
)

// PathNode 是事实发生时路径上的一截：当时的身份和名称。
type PathNode struct {
	ID     uuid.UUID  `json:"id"`               // 节点稳定身份
	TypeID *uuid.UUID `json:"typeId,omitempty"` // 旧快照可能带类型身份；新写入不再写
	Name   string     `json:"name"`             // 当时的显示名，之后改名也不改这里
}

// WorkContext 是新建资产时选的位置：只看文件夹，组织跟人走。
type WorkContext struct {
	FolderID uuid.UUID // 新建资产要挂的文件夹；空则挂对应树根
}

// PathSnapshot 按当前树从工厂走到该节点，记下当时身份和名称；写入后当原文。
func (s *Store) PathSnapshot(ctx context.Context, unitID uuid.UUID) ([]PathNode, error) {
	// 准备承接这次要交回的结果。
	var chain []PathNode
	// 从当前节点往上收集，直到厂根。
	current := unitID
	// 准备去重或保留集合，避免重复和误删。
	seen := map[uuid.UUID]struct{}{}
	// 一直往上走，遇到环或到根就停。
	for {
		// 这个节点走过就停，避免组织环死循环。
		if _, ok := seen[current]; ok {
			return nil, domain.ErrCycle
		}
		// 记下走过的节点，下一圈用来认环。
		seen[current] = struct{}{}
		// 先拿到这一步的结果，后面断言还要用。
		u, err := s.getUnit(ctx, current)
		// 读取组织节点失败就停，避免带着错误继续。
		if err != nil {
			return nil, err
		}
		// 收进结果，保持原来的先后顺序。
		chain = append(chain, PathNode{ID: u.ID, Name: u.Name})
		// 已经是根就停，根不能改父级或删除。
		if u.ParentID == nil {
			break
		}
		// 继续走向父级，直到根或发现环。
		current = *u.ParentID
	}
	// 把自叶到根的顺序倒成自根到叶。
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		// 交换两截，完成从叶到根的倒序。
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// 两边可空身份是否指向同一条。
func sameOptUUID(a, b *uuid.UUID) bool {
	// 两边都没有就视为相同。
	if a == nil && b == nil {
		return true
	}
	// 只有一边有就视为已经变化。
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
