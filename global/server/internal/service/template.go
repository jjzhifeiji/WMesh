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
	// 解析这段文本，失败就不能继续。
	sch, err := contenttpl.Parse(t.Schema)
	// 格式不对就拒绝，不接收坏数据。
	if err != nil {
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	// 编成字节再送出。
	canon, err := contenttpl.Marshal(sch)
	// 编不出就拒绝，不发送半截。
	if err != nil {
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	// 摘要对不上当篡改。
	if !digest.Match(canon, t.Digest) {
		return ContentTemplate{}, domain.ErrIntegrity
	}
	// 换成规范字段表，避免同一模版两套写法。
	t.Schema = canon
	return t, nil
}

// ensureTemplate 读工艺模版；没有则写入设备默认字段表。
func (s *kernel) ensureTemplate(ctx context.Context, kind string) (ContentTemplate, error) {
	// 不是工艺就拒绝，这项只对工艺开放。
	if kind != KindProcess {
		return ContentTemplate{}, domain.ErrNotFound
	}
	// 按字段模版处理。
	t, err := s.store.TemplateByKind(ctx, kind)
	// 没有错误才继续，有错留在后面的分支。
	if err == nil {
		// 解开模版正文，失败就不能继续。
		return openTemplate(t)
	}
	// 不是没有这条，就当真正的故障返回。
	if !errors.Is(err, domain.ErrNotFound) {
		return ContentTemplate{}, err
	}
	// 用设备默认字段表首次落库。
	schema, err := contenttpl.Marshal(contenttpl.Default(kind))
	// 编不出就拒绝，不发送半截。
	if err != nil {
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	// 写入这一条，失败就不能继续。
	row, err := s.store.InsertTemplate(ctx, ContentTemplate{Kind: kind, Schema: schema, Digest: digest.Sum(schema)})
	// 写入失败就停，避免留下半截。
	if err != nil {
		return ContentTemplate{}, err
	}
	// 解开模版正文，失败就不能继续。
	return openTemplate(row)
}

// ensureProjectItems 工程模版多份独立行；旧登记簿拆开，空库落下三份明细，三份字段随代码补齐。
func (s *kernel) ensureProjectItems(ctx context.Context) ([]ContentTemplate, error) {
	// 按字段模版处理。
	rows, err := s.store.TemplatesByKind(ctx, KindProject)
	// 没有或不合法就拒绝。
	if err != nil {
		return nil, err
	}
	// 先留空，读到当前模版再决定补还是留。
	var current []ContentTemplate
	// 先留空，不再使用的模版身份收在这里。
	var stale []uuid.UUID
	// 逐行整理，坏的一行就整批拒绝。
	for _, row := range rows {
		// 解析这段文本，失败就不能继续。
		sch, parseErr := contenttpl.Parse(row.Schema)
		// 字段表根不是对象则拒绝保存。
		if parseErr != nil || sch.Root != contenttpl.RootObject {
			// 把旧结构展开成现在的条目。
			items := contenttpl.ExpandLegacyProject(sch)
			// 展不开就拒绝，不能半截入库。
			if parseErr != nil {
				// 整理旧数据的路径或条目。
				items = contenttpl.LegacyProjectItems()
			}
			// 逐项检查，不合法就整份拒绝。
			for _, it := range items {
				// 解析这段文本，失败就不能继续。
				id, idErr := uuid.Parse(it.ID)
				// 格式不对就拒绝，不接收坏数据。
				if idErr != nil {
					return nil, domain.ErrTemplateInvalid
				}
				// 编成字节再送出。
				canon, mErr := contenttpl.Marshal(contenttpl.ObjectSchema(it.Fields))
				// 编不出就拒绝，不发送半截。
				if mErr != nil {
					return nil, domain.ErrTemplateInvalid
				}
				// 写入这一条，失败就不能继续。
				inserted, insErr := s.store.InsertTemplate(ctx, ContentTemplate{
					ID: id, Kind: KindProject, Name: it.Name, Schema: canon, Digest: digest.Sum(canon),
				})
				// 这一步失败就停，避免留下半截。
				if insErr != nil {
					// 按字段模版处理。
					got, getErr := s.store.TemplateByID(ctx, id)
					// 没有或不合法就拒绝。
					if getErr != nil {
						return nil, insErr
					}
					// 库里已有同份就用那一条，不再插第二遍。
					inserted = got
				}
				// 解开模版正文，失败就不能继续。
				opened, openErr := openTemplate(inserted)
				// 坏的按损坏拒绝，不能下发。
				if openErr != nil {
					return nil, openErr
				}
				// 把这一项接进结果。
				current = append(current, opened)
			}
			// 把这一项接进结果。
			stale = append(stale, row.ID)
			continue
		}
		// 解开模版正文，失败就不能继续。
		opened, openErr := openTemplate(row)
		// 坏的按损坏拒绝，不能下发。
		if openErr != nil {
			return nil, openErr
		}
		// 把这一项接进结果。
		current = append(current, opened)
	}
	// 找一个还不冲突的名字。
	current = uniqueTemplates(current)
	// 逐条清掉过期模版，避免厂端拿到旧的。
	for _, id := range stale {
		// 不是没有这条，就当真正的故障返回。
		if err := s.store.DeleteTemplate(ctx, id); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}
	// 有内容才继续解析，空字节不当坏包。
	if len(current) > 0 {
		// 用种子补缺的，已经合法的不改。
		return s.refreshSeedProjectItems(ctx, current)
	}
	// 按数量先准备容器。
	out := make([]ContentTemplate, 0, len(contenttpl.SeedProjectItems()))
	// 按种子项补齐，缺的就建，已有的留着。
	for _, it := range contenttpl.SeedProjectItems() {
		// 解析这段文本，失败就不能继续。
		id, idErr := uuid.Parse(it.ID)
		// 格式不对就拒绝，不接收坏数据。
		if idErr != nil {
			return nil, domain.ErrTemplateInvalid
		}
		// 编成字节再送出。
		canon, mErr := contenttpl.Marshal(contenttpl.ObjectSchema(it.Fields))
		// 编不出就拒绝，不发送半截。
		if mErr != nil {
			return nil, domain.ErrTemplateInvalid
		}
		// 写入这一条，失败就不能继续。
		inserted, insErr := s.store.InsertTemplate(ctx, ContentTemplate{
			ID: id, Kind: KindProject, Name: it.Name, Schema: canon, Digest: digest.Sum(canon),
		})
		// 这一步失败就停，避免留下半截。
		if insErr != nil {
			// 按字段模版处理。
			got, getErr := s.store.TemplateByID(ctx, id)
			// 没有或不合法就拒绝。
			if getErr != nil {
				return nil, insErr
			}
			// 库里已有同份就用那一条，不再插第二遍。
			inserted = got
		}
		// 解开模版正文，失败就不能继续。
		opened, openErr := openTemplate(inserted)
		// 坏的按损坏拒绝，不能下发。
		if openErr != nil {
			return nil, openErr
		}
		// 把这一项接进结果。
		out = append(out, opened)
	}
	// 找一个还不冲突的名字。
	return uniqueTemplates(out), nil
}

// refreshSeedProjectItems 空库三份字段表随代码补齐；自定义份不动。
func (s *kernel) refreshSeedProjectItems(ctx context.Context, current []ContentTemplate) ([]ContentTemplate, error) {
	// 按数量先准备容器。
	out := make([]ContentTemplate, len(current))
	// 复制一份，原件保持不动。
	copy(out, current)
	// 逐行收成视图，不把人员或正文带出去。
	for i, row := range out {
		// 用内置种子，失败就不能继续。
		seed, ok := contenttpl.SeedProjectItem(row.ID.String())
		// 对不上就跳过或拒绝，避免用错那一条。
		if !ok {
			continue
		}
		// 编成字节再送出。
		canon, err := contenttpl.Marshal(contenttpl.ObjectSchema(seed.Fields))
		// 编不出就拒绝，不发送半截。
		if err != nil {
			return nil, domain.ErrTemplateInvalid
		}
		// 摘要对不上则按损坏拒绝，不能入库或下发。
		if digest.Match(canon, row.Digest) {
			continue
		}
		// 更新这一条，修订不符则不行。
		updated, err := s.store.UpdateTemplateByID(ctx, row.ID, row.Revision, row.Name, canon, digest.Sum(canon))
		// 更新失败就停，避免写成半新半旧。
		if err != nil {
			return nil, err
		}
		// 解开模版正文，失败就不能继续。
		opened, err := openTemplate(updated)
		// 坏的按损坏拒绝，不能下发。
		if err != nil {
			return nil, err
		}
		// 解开后放回原位，调用方拿到明文模版。
		out[i] = opened
		// 通知厂端去拉模版。
		s.notifyTemplate(ctx, opened.ID, opened.Kind, opened.Revision)
	}
	return out, nil
}

// uniqueTemplates 同一身份只留一份。
func uniqueTemplates(rows []ContentTemplate) []ContentTemplate {
	// 用来挡住同一键被写两次。
	seen := map[uuid.UUID]struct{}{}
	// 按数量先准备容器。
	out := make([]ContentTemplate, 0, len(rows))
	// 逐行整理，坏的一行就整批拒绝。
	for _, row := range rows {
		// 同一键已经见过则拒绝，防止写两遍。
		if _, ok := seen[row.ID]; ok {
			continue
		}
		// 这份模版已经收过，避免重复下发。
		seen[row.ID] = struct{}{}
		// 把这一项接进结果。
		out = append(out, row)
	}
	return out
}

// projectItemSchemas 当前各份对象字段表。
func (s *kernel) projectItemSchemas(ctx context.Context) ([]contenttpl.ProjectItemSchema, error) {
	// 工程项不齐就按种子补。
	rows, err := s.ensureProjectItems(ctx)
	// 补失败就拒绝，模版不能缺层。
	if err != nil {
		return nil, err
	}
	// 按数量先准备容器。
	out := make([]contenttpl.ProjectItemSchema, 0, len(rows))
	// 逐行整理，坏的一行就整批拒绝。
	for _, row := range rows {
		// 解析这段文本，失败就不能继续。
		sch, err := contenttpl.Parse(row.Schema)
		// 字段表根不是对象则拒绝保存。
		if err != nil || sch.Root != contenttpl.RootObject {
			return nil, domain.ErrTemplateInvalid
		}
		// 把这一项接进结果。
		out = append(out, contenttpl.ProjectItemSchema{ID: row.ID.String(), Name: row.Name, Fields: sch.Fields})
	}
	return out, nil
}

// normalizeContent 新建时按当前模版套正文；没有合法模版则失败。
func (s *kernel) normalizeContent(ctx context.Context, kind string, content []byte) ([]byte, error) {
	// 按是不是工程决定要不要核对焊道和依赖。
	if kind == KindProject {
		// 新建工程不得再带路径键。
		if err := contenttpl.RejectProcessPath(content); err != nil {
			return nil, domain.ErrForbidden
		}
		// 按工程规则取名或核对。
		items, err := s.projectItemSchemas(ctx)
		// 不合法就拒绝，失败就不能继续。
		if err != nil {
			return nil, err
		}
		// 套进当前内容，失败就不能继续。
		out, err := contenttpl.ApplyProjectItems(items, content)
		// 套不上就拒绝，避免半新正文。
		if err != nil {
			return nil, domain.ErrTemplateInvalid
		}
		return out, nil
	}
	// 没有这份模版就补上种子。
	tpl, err := s.ensureTemplate(ctx, kind)
	// 补失败则厂端会缺字段。
	if err != nil {
		return nil, err
	}
	out, err := contenttpl.Apply(tpl.Schema, content) // 按当前模版套正文。
	// 套不上就拒绝，避免半新正文。
	if err != nil {
		return nil, domain.ErrTemplateInvalid
	}
	return out, nil
}

// 审计对象：类型加修订。
func templateTarget(kind string, rev int64) string {
	// 把数字收成文本。
	return kind + " rev=" + strconv.FormatInt(rev, 10)
}

// projectTemplateTarget 审计对象：工程模版名称加修订。
func projectTemplateTarget(name string, rev int64) string {
	// 把数字收成文本。
	return "project " + name + " rev=" + strconv.FormatInt(rev, 10)
}

// normalizeProjectName 去掉首尾空白，空或过长拒绝。
func normalizeProjectName(name string) (string, error) {
	// 去掉多余空白或前后缀。
	name = strings.TrimSpace(name)
	// 字数不在允许范围就拒绝，避免空名或超长。
	if name == "" || utf8.RuneCountInString(name) > maxTplNameRunes {
		return "", domain.ErrTemplateInvalid
	}
	return name, nil
}

// parseProjectSchema 工程模版必须是对象根；空则空字段表。
func parseProjectSchema(raw []byte) ([]byte, error) {
	// 空的就按没有处理，避免交出空壳当成功。
	if len(bytes.TrimSpace(raw)) == 0 {
		// 收成字节，失败就不能继续。
		raw = []byte(`{"root":"object"}`)
	}
	// 解析这段文本，失败就不能继续。
	sch, err := contenttpl.Parse(raw)
	// 字段表根不是对象则拒绝保存。
	if err != nil || sch.Root != contenttpl.RootObject {
		return nil, domain.ErrTemplateInvalid
	}
	// 编成字节再送出。
	return contenttpl.Marshal(sch)
}

// BuiltinSchema 读代码里的默认工艺字段表，不改已落库模版。
func (s *Templates) BuiltinSchema(ctx context.Context, token, kind string) ([]byte, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 读模版被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "get_template", kind+" default", audit.Deny)
		return nil, err
	}
	// 不是工艺就拒绝，这项只对工艺开放。
	if kind != KindProcess {
		// 读模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_template", kind+" default", audit.Deny)
		return nil, domain.ErrNotFound
	}
	// 编成字节再送出。
	raw, err := contenttpl.Marshal(contenttpl.Default(kind))
	// 编不出就拒绝，不发送半截。
	if err != nil {
		// 读模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_template", kind+" default", audit.Deny)
		return nil, domain.ErrTemplateInvalid
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "get_template", kind+" default", audit.Allow); err != nil {
		return nil, err
	}
	return raw, nil
}

// GetTemplate 读工艺当前模版；没有则写入设备默认字段表。
func (s *Templates) GetTemplate(ctx context.Context, token, kind string) (ContentTemplate, error) {
	// 只有 WAN 管理员能读模版。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 读模版被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	// 不是工艺就拒绝，这项只对工艺开放。
	if kind != KindProcess {
		// 读模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_template", kind, audit.Deny)
		return ContentTemplate{}, domain.ErrNotFound
	}
	// 没有这份模版就补上种子。
	t, err := s.ensureTemplate(ctx, kind)
	// 补失败则厂端会缺字段。
	if err != nil {
		// 读模版被拒就留审计。
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
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 读模版被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "get_template", KindProject, audit.Deny)
		return nil, err
	}
	// 工程项不齐就按种子补。
	rows, err := s.ensureProjectItems(ctx)
	// 补失败就拒绝，模版不能缺层。
	if err != nil {
		// 读模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_template", KindProject, audit.Deny)
		return nil, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "get_template", KindProject, audit.Allow); err != nil {
		return nil, err
	}
	return rows, nil
}

// CreateProjectTemplate 新建一份工程模版；可带空库种子身份补建缺失份。
func (s *Templates) CreateProjectTemplate(ctx context.Context, token string, id uuid.UUID, name string, schemaJSON []byte) (ContentTemplate, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "update_template", KindProject, audit.Deny)
		return ContentTemplate{}, err
	}
	// 身份是空就按未设置处理，避免写空号。
	if id != uuid.Nil {
		// 用内置种子，失败就不能继续。
		seed, ok := contenttpl.SeedProjectItem(id.String())
		// 对不上就跳过或拒绝，避免用错那一条。
		if !ok {
			// 改模版被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", KindProject, audit.Deny)
			return ContentTemplate{}, domain.ErrTemplateInvalid
		}
		// 按字段模版处理。
		_, err = s.store.TemplateByID(ctx, id)
		// 没有错误才继续，有错留在后面的分支。
		if err == nil {
			// 改模版被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", seed.Name, audit.Deny)
			return ContentTemplate{}, domain.ErrTemplateInvalid
		}
		// 不是没有这条，就当真正的故障返回。
		if !errors.Is(err, domain.ErrNotFound) {
			// 改模版被拒就留审计。
			_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", seed.Name, audit.Deny)
			return ContentTemplate{}, err
		}
		// 去掉空白后仍空则按未填或拒绝。
		if strings.TrimSpace(name) == "" {
			// 名字空了就用种子名，避免空白模版。
			name = seed.Name
		}
		// 空的就按没有处理，避免交出空壳当成功。
		if len(bytes.TrimSpace(schemaJSON)) == 0 {
			// 编成字节再送出。
			schemaJSON, err = contenttpl.Marshal(contenttpl.ObjectSchema(seed.Fields))
			// 编不出就拒绝，不发送半截。
			if err != nil {
				// 改模版被拒就留审计。
				_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
				return ContentTemplate{}, domain.ErrTemplateInvalid
			}
		}
	}
	// 收成合法值，空或太长不要。
	name, err = normalizeProjectName(name)
	// 不合法就拒绝，避免脏数据入库。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", KindProject, audit.Deny)
		return ContentTemplate{}, err
	}
	// 解析这段结构，失败就不能继续。
	canon, err := parseProjectSchema(schemaJSON)
	// 不合法就拒绝，不能保存。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	// 工程项不齐就按种子补。
	cur, err := s.ensureProjectItems(ctx)
	// 补失败就拒绝，模版不能缺层。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	// 超过上限则拒绝，挡住异常大批量。
	if len(cur) >= maxProjectTemplates {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	// 写入这一条，失败就不能继续。
	row, err := s.store.InsertTemplate(ctx, ContentTemplate{
		ID: id, Kind: KindProject, Name: name, Schema: canon, Digest: digest.Sum(canon),
	})
	// 这一步失败就停，避免留下半截。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	// 解开模版正文，失败就不能继续。
	row, err = openTemplate(row)
	// 坏的按损坏拒绝，不能下发。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(row.Name, row.Revision), audit.Allow); err != nil {
		return ContentTemplate{}, err
	}
	// 通知厂端去拉模版。
	s.notifyTemplate(ctx, row.ID, row.Kind, row.Revision)
	return row, nil
}

// UpdateProjectTemplate 改一份工程模版的名称和字段表，只升这一份修订。
func (s *Templates) UpdateProjectTemplate(ctx context.Context, token string, id uuid.UUID, expected int64, name string, schemaJSON []byte) (ContentTemplate, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "update_template", KindProject, audit.Deny)
		return ContentTemplate{}, err
	}
	// 收成合法值，空或太长不要。
	name, err = normalizeProjectName(name)
	// 不合法就拒绝，避免脏数据入库。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", KindProject, audit.Deny)
		return ContentTemplate{}, err
	}
	// 解析这段结构，失败就不能继续。
	canon, err := parseProjectSchema(schemaJSON)
	// 不合法就拒绝，不能保存。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	// 按字段模版处理。
	cur, err := s.store.TemplateByID(ctx, id)
	// 没有或不合法就拒绝。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	// 按是不是工程决定要不要核对焊道和依赖。
	if cur.Kind != KindProject {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, domain.ErrNotFound
	}
	// 解开模版正文，失败就不能继续。
	cur, err = openTemplate(cur)
	// 坏的按损坏拒绝，不能下发。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", name, audit.Deny)
		return ContentTemplate{}, err
	}
	// 修订对不上就拒绝，防止盖掉别人刚写的。
	if expected != cur.Revision {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(name, expected), audit.Deny)
		return ContentTemplate{}, domain.ErrRevisionConflict
	}
	// 字段表已经一样就不用再写，避免空改修订。
	if cur.Name == name && bytes.Equal(cur.Schema, canon) {
		// 审计没记下则中止，不当这次已经成功。
		if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(cur.Name, cur.Revision), audit.Allow); err != nil {
			return ContentTemplate{}, err
		}
		return cur, nil
	}
	// 更新这一条，修订不符则不行。
	row, err := s.store.UpdateTemplateByID(ctx, id, expected, name, canon, digest.Sum(canon))
	// 更新失败就停，避免写成半新半旧。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(name, expected), audit.Deny)
		return ContentTemplate{}, err
	}
	// 解开模版正文，失败就不能继续。
	row, err = openTemplate(row)
	// 坏的按损坏拒绝，不能下发。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(name, expected), audit.Deny)
		return ContentTemplate{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(row.Name, row.Revision), audit.Allow); err != nil {
		return ContentTemplate{}, err
	}
	// 通知厂端去拉模版。
	s.notifyTemplate(ctx, row.ID, row.Kind, row.Revision)
	return row, nil
}

// DeleteProjectTemplate 删掉一份当前工程模版，不改已有正文。
func (s *Templates) DeleteProjectTemplate(ctx context.Context, token string, id uuid.UUID) error {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "update_template", KindProject, audit.Deny)
		return err
	}
	// 按字段模版处理。
	cur, err := s.store.TemplateByID(ctx, id)
	// 没有或不合法就拒绝。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", KindProject, audit.Deny)
		return err
	}
	// 按是不是工程决定要不要核对焊道和依赖。
	if cur.Kind != KindProject {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", KindProject, audit.Deny)
		return domain.ErrNotFound
	}
	// 删除失败就停，避免库里留下残行。
	if err := s.store.DeleteTemplate(ctx, id); err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(cur.Name, cur.Revision), audit.Deny)
		return err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", projectTemplateTarget(cur.Name, cur.Revision), audit.Allow); err != nil {
		return err
	}
	// 通知厂端去拉或收回。
	s.notifyRemainingTemplates(ctx)
	return nil
}

// UpdateTemplate 保存工艺字段表并下发；不改已有正文。
func (s *Templates) UpdateTemplate(ctx context.Context, token, kind string, expected int64, schemaJSON []byte) (ContentTemplate, error) {
	// 只有 WAN 管理员能改字段表。失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "update_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	// 不是工艺就拒绝，这项只对工艺开放。
	if kind != KindProcess {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", kind, audit.Deny)
		return ContentTemplate{}, domain.ErrNotFound
	}
	sch, err := contenttpl.Parse(schemaJSON) // 收成规范字段表。
	// 格式不对就拒绝，不接收坏数据。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", kind, audit.Deny)
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	// 编成字节再送出。
	canon, err := contenttpl.Marshal(sch)
	// 编不出就拒绝，不发送半截。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", kind, audit.Deny)
		return ContentTemplate{}, domain.ErrTemplateInvalid
	}
	// 没有这份模版就补上种子。
	cur, err := s.ensureTemplate(ctx, kind)
	// 补失败则厂端会缺字段。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", kind, audit.Deny)
		return ContentTemplate{}, err
	}
	// 修订对不上则拒绝覆盖。
	if expected != cur.Revision {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", templateTarget(kind, expected), audit.Deny)
		return ContentTemplate{}, domain.ErrRevisionConflict
	}
	// 字段表已经一样就不用再写，避免空改修订。
	if bytes.Equal(cur.Schema, canon) {
		// 字段没变则幂等返回。
		if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", templateTarget(cur.Kind, cur.Revision), audit.Allow); err != nil {
			return ContentTemplate{}, err
		}
		return cur, nil
	}
	// 保存新字段表并升高修订，不改已有正文。
	row, err := s.store.UpdateTemplate(ctx, kind, expected, canon, digest.Sum(canon))
	// 更新失败就停，避免写成半新半旧。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", templateTarget(kind, expected), audit.Deny)
		return ContentTemplate{}, err
	}
	// 解开模版正文，失败就不能继续。
	row, err = openTemplate(row)
	// 坏的按损坏拒绝，不能下发。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", templateTarget(kind, expected), audit.Deny)
		return ContentTemplate{}, err
	}
	// 保存成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", templateTarget(row.Kind, row.Revision), audit.Allow); err != nil {
		return ContentTemplate{}, err
	}
	// 通知厂端去拉模版。
	s.notifyTemplate(ctx, row.ID, row.Kind, row.Revision)
	return row, nil
}

// SnapshotsForFactory 组当前工艺一份加全部工程模版快照。
func (s *Templates) SnapshotsForFactory(ctx context.Context, factoryID uuid.UUID) ([]TemplateSnapshot, error) {
	// 记下工厂身份，授权和审计都按这家厂。
	fid := factoryID
	// 没有这份模版就补上种子。
	proc, err := s.ensureTemplate(ctx, KindProcess)
	// 补失败则厂端会缺字段。
	if err != nil {
		return nil, err
	}
	// 工程项不齐就按种子补。
	projects, err := s.ensureProjectItems(ctx)
	// 补失败就拒绝，模版不能缺层。
	if err != nil {
		return nil, err
	}
	// 按数量先准备容器。
	out := make([]TemplateSnapshot, 0, 1+len(projects))
	// 把这一项接进结果。
	out = append(out, TemplateSnapshot{
		ID: proc.ID, Kind: proc.Kind, Name: proc.Name, Revision: proc.Revision,
		Schema: json.RawMessage(proc.Schema), Digest: proc.Digest, TargetFactoryID: &fid,
	})
	// 逐个工程套上当前模版，套不上就拒绝。
	for _, t := range projects {
		// 把这一项接进结果。
		out = append(out, TemplateSnapshot{
			ID: t.ID, Kind: t.Kind, Name: t.Name, Revision: t.Revision,
			Schema: json.RawMessage(t.Schema), Digest: t.Digest, TargetFactoryID: &fid,
		})
	}
	return out, nil
}

// SnapshotForFactory 组这一份模版给该厂，修订升高时只推这一条。
func (s *Templates) SnapshotForFactory(ctx context.Context, factoryID, templateID uuid.UUID) (TemplateSnapshot, error) {
	// 按字段模版处理。
	row, err := s.store.TemplateByID(ctx, templateID)
	// 没有或不合法就拒绝。
	if err != nil {
		return TemplateSnapshot{}, err
	}
	// 解开模版正文，失败就不能继续。
	opened, err := openTemplate(row)
	// 坏的按损坏拒绝，不能下发。
	if err != nil {
		return TemplateSnapshot{}, err
	}
	// 记下工厂身份，授权和审计都按这家厂。
	fid := factoryID
	return TemplateSnapshot{
		ID: opened.ID, Kind: opened.Kind, Name: opened.Name, Revision: opened.Revision,
		Schema: json.RawMessage(opened.Schema), Digest: opened.Digest, TargetFactoryID: &fid,
	}, nil
}
