package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
)

const maxProjectTemplates = 50 // 工程模版份数上限
const maxTplNameRunes = 80     // 名称字数上限

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

// ensureTemplate 读工艺模版；没有则写入设备默认字段表。
func (s *kernel) ensureTemplate(ctx context.Context, kind string) (ContentTemplate, error) {
	if kind != KindProcess {
		return ContentTemplate{}, domain.ErrNotFound
	}
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

// ensureProjectItems 工程模版多份独立行；旧登记簿拆开，空库落下三份明细。
func (s *kernel) ensureProjectItems(ctx context.Context) ([]ContentTemplate, error) {
	rows, err := s.store.TemplatesByKind(ctx, KindProject)
	if err != nil {
		return nil, err
	}
	var current []ContentTemplate
	var stale []uuid.UUID
	for _, row := range rows {
		sch, parseErr := contenttpl.Parse(row.Schema)
		if parseErr != nil || sch.Root != contenttpl.RootObject {
			items := contenttpl.ExpandLegacyProject(sch)
			if parseErr != nil {
				items = contenttpl.SeedProjectItems()
			}
			for _, it := range items {
				id, idErr := uuid.Parse(it.ID)
				if idErr != nil {
					return nil, domain.ErrTemplateInvalid
				}
				canon, mErr := contenttpl.Marshal(contenttpl.ObjectSchema(it.Fields))
				if mErr != nil {
					return nil, domain.ErrTemplateInvalid
				}
				inserted, insErr := s.store.InsertTemplate(ctx, ContentTemplate{
					ID: id, Kind: KindProject, Name: it.Name, Schema: canon, Digest: digest.Sum(canon),
				})
				if insErr != nil {
					got, getErr := s.store.TemplateByID(ctx, id)
					if getErr != nil {
						return nil, insErr
					}
					inserted = got
				}
				opened, openErr := openTemplate(inserted)
				if openErr != nil {
					return nil, openErr
				}
				current = append(current, opened)
			}
			stale = append(stale, row.ID)
			continue
		}
		opened, openErr := openTemplate(row)
		if openErr != nil {
			return nil, openErr
		}
		current = append(current, opened)
	}
	current = uniqueTemplates(current)
	for _, id := range stale {
		if err := s.store.DeleteTemplate(ctx, id); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}
	if len(current) > 0 {
		return current, nil
	}
	out := make([]ContentTemplate, 0, 3)
	for _, it := range contenttpl.SeedProjectItems() {
		id, idErr := uuid.Parse(it.ID)
		if idErr != nil {
			return nil, domain.ErrTemplateInvalid
		}
		canon, mErr := contenttpl.Marshal(contenttpl.ObjectSchema(it.Fields))
		if mErr != nil {
			return nil, domain.ErrTemplateInvalid
		}
		inserted, insErr := s.store.InsertTemplate(ctx, ContentTemplate{
			ID: id, Kind: KindProject, Name: it.Name, Schema: canon, Digest: digest.Sum(canon),
		})
		if insErr != nil {
			got, getErr := s.store.TemplateByID(ctx, id)
			if getErr != nil {
				return nil, insErr
			}
			inserted = got
		}
		opened, openErr := openTemplate(inserted)
		if openErr != nil {
			return nil, openErr
		}
		out = append(out, opened)
	}
	return uniqueTemplates(out), nil
}

// uniqueTemplates 同一身份只留一份。
func uniqueTemplates(rows []ContentTemplate) []ContentTemplate {
	seen := map[uuid.UUID]struct{}{}
	out := make([]ContentTemplate, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ID]; ok {
			continue
		}
		seen[row.ID] = struct{}{}
		out = append(out, row)
	}
	return out
}

// projectItemSchemas 当前各份对象字段表。
func (s *kernel) projectItemSchemas(ctx context.Context) ([]contenttpl.ProjectItemSchema, error) {
	rows, err := s.ensureProjectItems(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]contenttpl.ProjectItemSchema, 0, len(rows))
	for _, row := range rows {
		sch, err := contenttpl.Parse(row.Schema)
		if err != nil || sch.Root != contenttpl.RootObject {
			return nil, domain.ErrTemplateInvalid
		}
		out = append(out, contenttpl.ProjectItemSchema{ID: row.ID.String(), Name: row.Name, Fields: sch.Fields})
	}
	return out, nil
}

// normalizeContent 新建时按当前模版套正文；没有合法模版则失败。
func (s *kernel) normalizeContent(ctx context.Context, kind string, content []byte) ([]byte, error) {
	if kind == KindProject {
		items, err := s.projectItemSchemas(ctx)
		if err != nil {
			return nil, err
		}
		out, err := contenttpl.ApplyProjectItems(items, content)
		if err != nil {
			return nil, domain.ErrTemplateInvalid
		}
		return out, nil
	}
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

// projectTemplateTarget 审计对象：工程模版名称加修订。
func projectTemplateTarget(name string, rev int64) string {
	return "project " + name + " rev=" + strconv.FormatInt(rev, 10)
}

// normalizeProjectName 去掉首尾空白，空或过长拒绝。
func normalizeProjectName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxTplNameRunes {
		return "", domain.ErrTemplateInvalid
	}
	return name, nil
}

// parseProjectSchema 工程模版必须是对象根；空则空字段表。
func parseProjectSchema(raw []byte) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte(`{"root":"object"}`)
	}
	sch, err := contenttpl.Parse(raw)
	if err != nil || sch.Root != contenttpl.RootObject {
		return nil, domain.ErrTemplateInvalid
	}
	return contenttpl.Marshal(sch)
}

// BuiltinSchema 读代码里的默认工艺字段表，不改已落库模版。
func (s *Templates) BuiltinSchema(ctx context.Context, token, kind string) ([]byte, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "get_template", kind+" default", audit.Deny)
		return nil, err
	}
	if kind != KindProcess {
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

// GetTemplate 读工艺当前模版；没有则写入设备默认字段表。
func (s *Templates) GetTemplate(ctx context.Context, token, kind string) (ContentTemplate, error) {
	// 只有 WAN 管理员能读模版。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	if kind != KindProcess {
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

// ListProjectTemplates 列出当前各份独立工程模版。
func (s *Templates) ListProjectTemplates(ctx context.Context, token string) ([]ContentTemplate, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "get_template", KindProject, audit.Deny)
		return nil, err
	}
	rows, err := s.ensureProjectItems(ctx)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_template", KindProject, audit.Deny)
		return nil, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "get_template", KindProject, audit.Allow); err != nil {
		return nil, err
	}
	return rows, nil
}

// CreateProjectTemplate 新建一份工程模版，字段从空或所给对象表起。
func (s *Templates) CreateProjectTemplate(ctx context.Context, token, name string, schemaJSON []byte) (ContentTemplate, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "update_template", KindProject, audit.Deny)
		return ContentTemplate{}, err
	}
	name, err = normalizeProjectName(name)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", KindProject, audit.Deny)
		return ContentTemplate{}, err
	}
	canon, err := parseProjectSchema(schemaJSON)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	cur, err := s.ensureProjectItems(ctx)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	if len(cur) >= maxProjectTemplates {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	row, err := s.store.InsertTemplate(ctx, ContentTemplate{
		Kind: KindProject, Name: name, Schema: canon, Digest: digest.Sum(canon),
	})
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	row, err = openTemplate(row)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(row.Name, row.Revision), audit.Allow); err != nil {
		return ContentTemplate{}, err
	}
	return row, nil
}

// UpdateProjectTemplate 改一份工程模版的名称和字段表，只升这一份修订。
func (s *Templates) UpdateProjectTemplate(ctx context.Context, token string, id uuid.UUID, expected int64, name string, schemaJSON []byte) (ContentTemplate, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "update_template", KindProject, audit.Deny)
		return ContentTemplate{}, err
	}
	name, err = normalizeProjectName(name)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", KindProject, audit.Deny)
		return ContentTemplate{}, err
	}
	canon, err := parseProjectSchema(schemaJSON)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	cur, err := s.store.TemplateByID(ctx, id)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	if cur.Kind != KindProject {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, domain.ErrNotFound
	}
	cur, err = openTemplate(cur)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	if expected != cur.Revision {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(name, expected), audit.Deny)
		return ContentTemplate{}, domain.ErrRevisionConflict
	}
	if cur.Name == name && bytes.Equal(cur.Schema, canon) {
		if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(cur.Name, cur.Revision), audit.Allow); err != nil {
			return ContentTemplate{}, err
		}
		return cur, nil
	}
	row, err := s.store.UpdateTemplateByID(ctx, id, expected, name, canon, digest.Sum(canon))
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(name, expected), audit.Deny)
		return ContentTemplate{}, err
	}
	row, err = openTemplate(row)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(name, expected), audit.Deny)
		return ContentTemplate{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(row.Name, row.Revision), audit.Allow); err != nil {
		return ContentTemplate{}, err
	}
	return row, nil
}

// DeleteProjectTemplate 删掉一份当前工程模版，不改已有正文。
func (s *Templates) DeleteProjectTemplate(ctx context.Context, token string, id uuid.UUID) error {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "update_template", KindProject, audit.Deny)
		return err
	}
	cur, err := s.store.TemplateByID(ctx, id)
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", KindProject, audit.Deny)
		return err
	}
	if cur.Kind != KindProject {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", KindProject, audit.Deny)
		return domain.ErrNotFound
	}
	if err := s.store.DeleteTemplate(ctx, id); err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(cur.Name, cur.Revision), audit.Deny)
		return err
	}
	return s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(cur.Name, cur.Revision), audit.Allow)
}

// UpdateTemplate 保存工艺字段表并下发；不改已有正文。
func (s *Templates) UpdateTemplate(ctx context.Context, token, kind string, expected int64, schemaJSON []byte) (ContentTemplate, error) {
	// 只有 WAN 管理员能改字段表。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "update_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	if kind != KindProcess {
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

// SnapshotsForFactory 组当前工艺一份加全部工程模版快照。
func (s *Templates) SnapshotsForFactory(ctx context.Context, factoryID uuid.UUID) ([]TemplateSnapshot, error) {
	fid := factoryID
	proc, err := s.ensureTemplate(ctx, KindProcess)
	if err != nil {
		return nil, err
	}
	projects, err := s.ensureProjectItems(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TemplateSnapshot, 0, 1+len(projects))
	out = append(out, TemplateSnapshot{
		ID: proc.ID, Kind: proc.Kind, Name: proc.Name, Revision: proc.Revision,
		Schema: json.RawMessage(proc.Schema), Digest: proc.Digest, TargetFactoryID: &fid,
	})
	for _, t := range projects {
		out = append(out, TemplateSnapshot{
			ID: t.ID, Kind: t.Kind, Name: t.Name, Revision: t.Revision,
			Schema: json.RawMessage(t.Schema), Digest: t.Digest, TargetFactoryID: &fid,
		})
	}
	return out, nil
}
