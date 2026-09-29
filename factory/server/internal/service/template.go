package service

import (
	"context"
	"errors"
	"strconv"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
)

// openTemplate 把库里的字段 JSON 收成规范形并核对摘要。
func openTemplate(t ContentTemplate) (ContentTemplate, error) {
	// 解析本机身份，避免留下不一致的结果
	sch, err := contenttpl.Parse(t.Schema)
	// 解析失败就不把现场钉到设备
	if err != nil {
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	// 收成稳定正文，避免留下不一致的结果
	canon, err := contenttpl.Marshal(sch)
	// 收不成正文则拒绝继续
	if err != nil {
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	// 条件不成立则拒绝或跳过，不往下改数据
	if !digest.Match(canon, t.Digest) {
		return ContentTemplate{}, domain.ErrIntegrity
	}
	// 换成核对过摘要的规范字段表
	t.Schema = canon
	return t, nil
}

// openedProjectItem 只收对象根且摘要对得上的份；旧登记簿丢掉，不挡当前各份。
func openedProjectItem(t ContentTemplate) (ContentTemplate, bool) {
	// 把字段表收成规范形并核对摘要
	row, err := openTemplate(t)
	// 摘要对不上则拒绝这份模版
	if err != nil {
		return ContentTemplate{}, false
	}
	// 解析本机身份，避免留下不一致的结果
	sch, err := contenttpl.Parse(row.Schema)
	// 解析失败就不把现场钉到设备
	if err != nil || sch.Root != contenttpl.RootObject {
		return ContentTemplate{}, false
	}
	return row, true
}

// projectItems 已收的对象根工程模版；没有则空。
func (s *kernel) projectItems(ctx context.Context) ([]contenttpl.ProjectItemSchema, error) {
	// 取各份工程模版的最高修订
	rows, err := s.store.LatestProjectTemplates(ctx)
	// 列表读失败则不返回残缺
	if err != nil {
		return nil, err
	}
	// 按条数决定是空、超限还是继续
	out := make([]contenttpl.ProjectItemSchema, 0, len(rows))
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, row := range rows {
		// 只收对象根且摘要对得上的份
		row, ok := openedProjectItem(row)
		// 没有取到则按缺失拒绝或留空
		if !ok {
			continue
		}
		// 解析本机身份，避免留下不一致的结果
		sch, _ := contenttpl.Parse(row.Schema)
		// 收成文本身份，供审计或对账对准
		out = append(out, contenttpl.ProjectItemSchema{ID: row.ID.String(), Name: row.Name, Fields: sch.Fields})
	}
	return out, nil
}

// normalizeContent 新建时按已收模版套正文；尚未收到副本则原样返回。
func (s *kernel) normalizeContent(ctx context.Context, kind string, content []byte) ([]byte, error) {
	// 不是工程则拒绝装袋或授权
	if kind == KindProject {
		// 新建工程不得再带路径键。
		if err := contenttpl.RejectProcessPath(content); err != nil {
			return nil, domain.ErrForbidden
		}
		// 取已收的对象根工程模版
		items, err := s.projectItems(ctx)
		// 模版读失败则正文先按原样
		if err != nil {
			return nil, err
		}
		// 没有条目就直接返回，不必再校验
		if len(items) == 0 {
			return content, nil
		}
		// 套上工程明细，避免留下不一致的结果
		out, err := contenttpl.ApplyProjectItems(items, content)
		// 套不上则正文保持原样
		if err != nil {
			return nil, domain.ErrTemplateInvalid
		}
		return out, nil
	}
	// 尚未收到模版副本则原样返回。
	tpl, err := s.store.LatestTemplateByKind(ctx, kind)
	// 还没收到这份模版，不把没有记录当成故障
	if errors.Is(err, domain.ErrNotFound) {
		return content, nil
	}
	// 分不清就按失败停住
	if err != nil {
		return nil, err
	}
	// 把字段表收成规范形并核对摘要
	tpl, err = openTemplate(tpl)
	// 摘要对不上则拒绝这份模版
	if err != nil {
		return nil, err
	}
	// 按已收模版套新建正文。
	out, err := contenttpl.Apply(tpl.Schema, content)
	// 写入失败则停住，避免留下半截状态
	if err != nil {
		return nil, domain.ErrTemplateInvalid
	}
	return out, nil
}

// collectProjectProcessIDs 有已收对象模版用各份字段；没有则用空库三份明细扫引用。
func (s *kernel) collectProjectProcessIDs(ctx context.Context, content []byte) ([]string, error) {
	// 取已收的对象根工程模版
	items, err := s.projectItems(ctx)
	// 模版读失败则正文先按原样
	if err != nil {
		return nil, err
	}
	// 没有条目就直接返回，不必再校验
	if len(items) == 0 {
		// 写入演示用的工程明细
		items = contenttpl.SeedProjectItems()
	}
	// 按当前模版收集引用；路径键直接拒绝。
	ids, err := contenttpl.CollectProcessIDsFromItems(items, content)
	// 命中这种预期错误
	if errors.Is(err, contenttpl.ErrProcessPath) {
		return nil, domain.ErrForbidden
	}
	return ids, err
}

// 审计对象：类型加修订。
func templateTarget(kind string, rev int64) string {
	// 把数字收成十进制文本，再交回调用方
	return kind + " rev=" + strconv.FormatInt(rev, 10)
}

// GetTemplate 读本厂已收工艺最高修订模版。
func (s *Templates) GetTemplate(ctx context.Context, token, kind string) (ContentTemplate, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return ContentTemplate{}, err
	}
	// 有效账号可读已收模版。失败记拒绝。
	if kind != KindProcess {
		// 记下读模版被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, domain.ErrNotFound
	}
	// 取该类型最高修订模版
	t, err := s.store.LatestTemplateByKind(ctx, kind)
	// 模版读失败则不能套正文
	if err != nil {
		// 记下读模版被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	// 把字段表收成规范形并核对摘要
	t, err = openTemplate(t)
	// 摘要对不上则拒绝这份模版
	if err != nil {
		// 记下读模版被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, nil, "get_template", templateTarget(t.Kind, t.Revision), audit.Allow); err != nil {
		return ContentTemplate{}, err
	}
	return t, nil
}

// ListProjectTemplates 列出本厂已收各份工程模版最高修订。
func (s *Templates) ListProjectTemplates(ctx context.Context, token string) ([]ContentTemplate, error) {
	// 先确认会话仍有效，过期或停用直接拒绝
	acc, err := s.RequireActive(ctx, token)
	// 会话无效或账号已停用，拒绝继续
	if err != nil {
		return nil, err
	}
	// 取各份工程模版的最高修订
	rows, err := s.store.LatestProjectTemplates(ctx)
	// 列表读失败则不返回残缺
	if err != nil {
		// 记下读模版被拒绝，写失败不改变结果
		_ = s.audit(ctx, &acc.ID, nil, "get_template", KindProject, audit.Deny)
		return nil, err
	}
	// 按条数决定是空、超限还是继续
	out := make([]ContentTemplate, 0, len(rows))
	// 逐行按调用方作用域裁，看不到的丢掉
	for _, row := range rows {
		// 只收对象根且摘要对得上的份
		row, ok := openedProjectItem(row)
		// 没有取到则按缺失拒绝或留空
		if !ok {
			continue
		}
		// 把这一条收进结果，漏了清单就不齐
		out = append(out, row)
	}
	// 审计没写下则整次不算完成
	if err := s.audit(ctx, &acc.ID, nil, "get_template", KindProject, audit.Allow); err != nil {
		return nil, err
	}
	return out, nil
}

// AcceptTemplateDelivery 把 WAN 送达的模版写入只读副本，不改已有正文。
func (s *Closure) AcceptTemplateDelivery(ctx context.Context, snap TemplateSnapshot) error {
	// 取出本厂稳定身份，封包和审计都要用
	fid := s.store.FactoryID()
	// 接收厂对不上本厂则拒绝收下
	if snap.TargetFactoryID == nil || *snap.TargetFactoryID != fid {
		// 接收方对不上，记下收下模版被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "accept_template", snap.Kind, audit.Deny)
		return domain.ErrForbidden
	}
	// 不是工程则拒绝装袋或授权
	if snap.Kind != KindProcess && snap.Kind != KindProject {
		// 种类或级别不符，记下收下模版被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "accept_template", snap.Kind, audit.Deny)
		return domain.ErrForbidden
	}
	// 摘要或字段表不对一律记拒绝。
	if !digest.Match([]byte(snap.Schema), snap.Digest) {
		// 记下收下模版被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "accept_template", snap.Kind, audit.Deny)
		return domain.ErrIntegrity
	}
	// 解析失败就不把现场钉到设备
	if _, err := contenttpl.Parse(snap.Schema); err != nil {
		// 记下收下模版被拒绝，写失败不改变结果
		_ = s.audit(ctx, nil, nil, "accept_template", snap.Kind, audit.Deny)
		return domain.ErrTemplateInvalid
	}
	// 只读副本，不改已有正文。
	if _, err := s.store.InsertTemplateReplica(ctx, ContentTemplate{
		// 记下身份、类型和修订，正文不在这行
		ID: snap.ID, Kind: snap.Kind, Name: snap.Name, Revision: snap.Revision,
		Schema: []byte(snap.Schema), Digest: snap.Digest,
	}); err != nil {
		_ = s.audit(ctx, nil, nil, "accept_template", templateTarget(snap.Kind, snap.Revision), audit.Deny)
		return err
	}
	// 收下模版成功后记审计，再把结果交回
	return s.audit(ctx, nil, nil, "accept_template", templateTarget(snap.Kind, snap.Revision), audit.Allow)
}
