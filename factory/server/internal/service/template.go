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
	sch, err := contenttpl.Parse(t.Schema)
	if err != nil {
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	canon, err := contenttpl.Marshal(sch)
	if err != nil {
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	if !digest.Match(canon, t.Digest) {
		return ContentTemplate{}, domain.ErrIntegrity
	}
	t.Schema = canon
	return t, nil
}

// projectItems 已收的对象根工程模版；没有则空。
func (s *kernel) projectItems(ctx context.Context) ([]contenttpl.ProjectItemSchema, error) {
	rows, err := s.store.LatestProjectTemplates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]contenttpl.ProjectItemSchema, 0, len(rows))
	for _, row := range rows {
		row, err = openTemplate(row)
		if err != nil {
			return nil, err
		}
		sch, err := contenttpl.Parse(row.Schema)
		if err != nil || sch.Root != contenttpl.RootObject {
			continue
		}
		out = append(out, contenttpl.ProjectItemSchema{ID: row.ID.String(), Name: row.Name, Fields: sch.Fields})
	}
	return out, nil
}

// normalizeContent 新建时按已收模版套正文；尚未收到副本则原样返回。
func (s *kernel) normalizeContent(ctx context.Context, kind string, content []byte) ([]byte, error) {
	if kind == KindProject {
		items, err := s.projectItems(ctx)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return content, nil
		}
		out, err := contenttpl.ApplyProjectItems(items, content)
		if err != nil {
			return nil, domain.ErrTemplateInvalid
		}
		return out, nil
	}
	// 尚未收到模版副本则原样返回。
	tpl, err := s.store.LatestTemplateByKind(ctx, kind)
	if errors.Is(err, domain.ErrNotFound) {
		return content, nil
	}
	if err != nil {
		return nil, err
	}
	tpl, err = openTemplate(tpl)
	if err != nil {
		return nil, err
	}
	// 按已收模版套新建正文。
	out, err := contenttpl.Apply(tpl.Schema, content)
	if err != nil {
		return nil, domain.ErrTemplateInvalid
	}
	return out, nil
}

// collectProjectProcessIDs 有已收对象模版用各份字段；没有则用空库四份明细扫引用。
func (s *kernel) collectProjectProcessIDs(ctx context.Context, content []byte) ([]string, error) {
	items, err := s.projectItems(ctx)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		items = contenttpl.SeedProjectItems()
	}
	return contenttpl.CollectProcessIDsFromItems(items, content)
}

// 审计对象：类型加修订。
func templateTarget(kind string, rev int64) string {
	return kind + " rev=" + strconv.FormatInt(rev, 10)
}

// GetTemplate 读本厂已收工艺最高修订模版。
func (s *Templates) GetTemplate(ctx context.Context, token, kind string) (ContentTemplate, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return ContentTemplate{}, err
	}
	// 有效账号可读已收模版。失败记拒绝。
	if kind != KindProcess {
		_ = s.audit(ctx, &acc.ID, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, domain.ErrNotFound
	}
	t, err := s.store.LatestTemplateByKind(ctx, kind)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	t, err = openTemplate(t)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	if err := s.audit(ctx, &acc.ID, nil, "get_template", templateTarget(t.Kind, t.Revision), audit.Allow); err != nil {
		return ContentTemplate{}, err
	}
	return t, nil
}

// ListProjectTemplates 列出本厂已收各份工程模版最高修订。
func (s *Templates) ListProjectTemplates(ctx context.Context, token string) ([]ContentTemplate, error) {
	acc, err := s.RequireActive(ctx, token)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.LatestProjectTemplates(ctx)
	if err != nil {
		_ = s.audit(ctx, &acc.ID, nil, "get_template", KindProject, audit.Deny)
		return nil, err
	}
	out := make([]ContentTemplate, 0, len(rows))
	for _, row := range rows {
		row, err = openTemplate(row)
		if err != nil {
			_ = s.audit(ctx, &acc.ID, nil, "get_template", KindProject, audit.Deny)
			return nil, err
		}
		sch, err := contenttpl.Parse(row.Schema)
		if err != nil || sch.Root != contenttpl.RootObject {
			continue
		}
		out = append(out, row)
	}
	if err := s.audit(ctx, &acc.ID, nil, "get_template", KindProject, audit.Allow); err != nil {
		return nil, err
	}
	return out, nil
}

// AcceptTemplateDelivery 把 WAN 送达的模版写入只读副本，不改已有正文。
func (s *Closure) AcceptTemplateDelivery(ctx context.Context, snap TemplateSnapshot) error {
	fid := s.store.FactoryID()
	if snap.TargetFactoryID == nil || *snap.TargetFactoryID != fid {
		_ = s.audit(ctx, nil, nil, "accept_template", snap.Kind, audit.Deny)
		return domain.ErrForbidden
	}
	if snap.Kind != KindProcess && snap.Kind != KindProject {
		_ = s.audit(ctx, nil, nil, "accept_template", snap.Kind, audit.Deny)
		return domain.ErrForbidden
	}
	// 摘要或字段表不对一律记拒绝。
	if !digest.Match([]byte(snap.Schema), snap.Digest) {
		_ = s.audit(ctx, nil, nil, "accept_template", snap.Kind, audit.Deny)
		return domain.ErrIntegrity
	}
	if _, err := contenttpl.Parse(snap.Schema); err != nil {
		_ = s.audit(ctx, nil, nil, "accept_template", snap.Kind, audit.Deny)
		return domain.ErrTemplateInvalid
	}
	// 只读副本，不改已有正文。
	if _, err := s.store.InsertTemplateReplica(ctx, ContentTemplate{
		ID: snap.ID, Kind: snap.Kind, Name: snap.Name, Revision: snap.Revision,
		Schema: []byte(snap.Schema), Digest: snap.Digest,
	}); err != nil {
		_ = s.audit(ctx, nil, nil, "accept_template", templateTarget(snap.Kind, snap.Revision), audit.Deny)
		return err
	}
	return s.audit(ctx, nil, nil, "accept_template", templateTarget(snap.Kind, snap.Revision), audit.Allow)
}
