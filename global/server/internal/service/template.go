package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
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
	// 摘要对不上当篡改。
	if !digest.Match(canon, t.Digest) {
		return ContentTemplate{}, domain.ErrIntegrity
	}
	t.Schema = canon
	return t, nil
}

// ensureTemplate 读该类型模版；没有则写入设备默认字段表。
func (s *kernel) ensureTemplate(ctx context.Context, kind string) (ContentTemplate, error) {
	t, err := s.store.TemplateByKind(ctx, kind)
	if err == nil {
		return openTemplate(t)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return ContentTemplate{}, err
	}
	// 用设备默认字段表首次落库。
	schema, err := contenttpl.Marshal(contenttpl.Default(kind))
	if err != nil {
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	row, err := s.store.InsertTemplate(ctx, ContentTemplate{Kind: kind, Schema: schema, Digest: digest.Sum(schema)})
	if err != nil {
		return ContentTemplate{}, err
	}
	return openTemplate(row)
}

// normalizeContent 新建时按当前模版套正文；没有合法模版则失败。
func (s *kernel) normalizeContent(ctx context.Context, kind string, content []byte) ([]byte, error) {
	tpl, err := s.ensureTemplate(ctx, kind)
	if err != nil {
		return nil, err
	}
	out, err := contenttpl.Apply(tpl.Schema, content) // 按当前模版套正文。
	if err != nil {
		return nil, domain.ErrTemplateInvalid
	}
	return out, nil
}

// 审计对象：类型加修订。
func templateTarget(kind string, rev int64) string {
	return kind + " rev=" + strconv.FormatInt(rev, 10)
}

// BuiltinSchema 读代码里的默认字段表，不改已落库模版。
func (s *Templates) BuiltinSchema(ctx context.Context, token, kind string) ([]byte, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "get_template", kind+" default", audit.Deny)
		return nil, err
	}
	if kind != KindProcess && kind != KindProject {
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_template", kind+" default", audit.Deny)
		return nil, domain.ErrNotFound
	}
	raw, err := contenttpl.Marshal(contenttpl.Default(kind))
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_template", kind+" default", audit.Deny)
		return nil, domain.ErrTemplateInvalid
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "get_template", kind+" default", audit.Allow); err != nil {
		return nil, err
	}
	return raw, nil
}

// GetTemplate 读该类型当前模版；没有则写入设备默认字段表。
func (s *Templates) GetTemplate(ctx context.Context, token, kind string) (ContentTemplate, error) {
	// 只有 WAN 管理员能读模版。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	if kind != KindProcess && kind != KindProject {
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, domain.ErrNotFound
	}
	t, err := s.ensureTemplate(ctx, kind)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	// 读取成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "get_template", templateTarget(t.Kind, t.Revision), audit.Allow); err != nil {
		return ContentTemplate{}, err
	}
	return t, nil
}

// UpdateTemplate 保存字段表并下发；不改已有正文。
func (s *Templates) UpdateTemplate(ctx context.Context, token, kind string, expected int64, schemaJSON []byte) (ContentTemplate, error) {
	// 只有 WAN 管理员能改字段表。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "update_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	if kind != KindProcess && kind != KindProject {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", kind, audit.Deny)
		return ContentTemplate{}, domain.ErrNotFound
	}
	sch, err := contenttpl.Parse(schemaJSON) // 收成规范字段表。
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", kind, audit.Deny)
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	canon, err := contenttpl.Marshal(sch)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", kind, audit.Deny)
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	cur, err := s.ensureTemplate(ctx, kind)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	// 修订对不上则拒绝覆盖。
	if expected != cur.Revision {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", templateTarget(kind, expected), audit.Deny)
		return ContentTemplate{}, domain.ErrRevisionConflict
	}
	if bytes.Equal(cur.Schema, canon) {
		// 字段没变则幂等返回。
		if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", templateTarget(cur.Kind, cur.Revision), audit.Allow); err != nil {
			return ContentTemplate{}, err
		}
		return cur, nil
	}
	// 保存新字段表并升高修订，不改已有正文。
	row, err := s.store.UpdateTemplate(ctx, kind, expected, canon, digest.Sum(canon))
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", templateTarget(kind, expected), audit.Deny)
		return ContentTemplate{}, err
	}
	row, err = openTemplate(row)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", templateTarget(kind, expected), audit.Deny)
		return ContentTemplate{}, err
	}
	// 保存成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", templateTarget(row.Kind, row.Revision), audit.Allow); err != nil {
		return ContentTemplate{}, err
	}
	return row, nil
}

// SnapshotsForFactory 组当前两份模版快照，给回连或改完下发。
func (s *Templates) SnapshotsForFactory(ctx context.Context, factoryID uuid.UUID) ([]TemplateSnapshot, error) {
	out := make([]TemplateSnapshot, 0, 2)
	fid := factoryID
	for _, kind := range []string{KindProcess, KindProject} {
		t, err := s.ensureTemplate(ctx, kind)
		if err != nil {
			return nil, err
		}
		out = append(out, TemplateSnapshot{
			ID: t.ID, Kind: t.Kind, Revision: t.Revision,
			Schema: json.RawMessage(t.Schema), Digest: t.Digest, TargetFactoryID: &fid,
		})
	}
	return out, nil
}
