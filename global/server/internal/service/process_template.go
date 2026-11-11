package service

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
)

// processTemplateTarget 审计对象：工艺模版名称加修订。
func processTemplateTarget(name string, rev int64) string {
	// 把数字收成文本。
	return "process " + name + " rev=" + strconv.FormatInt(rev, 10)
}

// ensureProcessTemplates 读当前全部工艺模版；这一类从未落过种子时落预置两份（焊接参数、包角），删掉的不补回。
func (s *kernel) ensureProcessTemplates(ctx context.Context) ([]ContentTemplate, error) {
	// 先看落没落过种子，落过就只读当前各份。
	seeded, err := s.store.TemplateSeeded(ctx, KindProcess)
	// 读标记失败就停，不能当成没落过再写一遍。
	if err != nil {
		return nil, err
	}
	// 准备接住当前各份。
	var rows []ContentTemplate
	// 没落过才走一次性种子；已有同身份的不覆盖。
	if !seeded {
		// 按预置份数先准备容器。
		seeds := make([]ContentTemplate, 0, len(contenttpl.SeedProcessTemplates()))
		// 逐份把代码里的预置收成待写模版。
		for _, it := range contenttpl.SeedProcessTemplates() {
			// 解析预置身份，失败就不能继续。
			id, idErr := uuid.Parse(it.ID)
			// 格式不对就拒绝，不接收坏数据。
			if idErr != nil {
				return nil, domain.ErrTemplateInvalid
			}
			// 编成规范字段表再落库。
			canon, mErr := contenttpl.Marshal(contenttpl.ObjectSchema(it.Fields))
			// 编不出就拒绝，不写半截种子。
			if mErr != nil {
				return nil, domain.ErrTemplateInvalid
			}
			// 把这一份接进待写批次。
			seeds = append(seeds, ContentTemplate{
				ID: id, Kind: KindProcess, Name: it.Name, Schema: canon, Digest: digest.Sum(canon),
			})
		}
		// 抢到标记才写，没抢到就读别人刚落的。
		rows, err = s.store.SeedTemplatesOnce(ctx, KindProcess, seeds)
	}
	// 落过种子就只读当前各份。
	if seeded {
		// 读这一类的当前各份。
		rows, err = s.store.TemplatesByKind(ctx, KindProcess)
	}
	// 落种子或读取失败就停，不能当成空表。
	if err != nil {
		return nil, err
	}
	// 按行数先准备容器。
	out := make([]ContentTemplate, 0, len(rows))
	// 逐行解开核对，坏的一行就整批拒绝。
	for _, row := range rows {
		// 解开模版正文，失败就不能继续。
		opened, openErr := openTemplate(row)
		// 坏的按损坏拒绝，不能下发。
		if openErr != nil {
			return nil, openErr
		}
		// 把这一项接进结果。
		out = append(out, opened)
	}
	return out, nil
}

// processTemplate 按身份取一份当前工艺模版；没有交回否，不算错。
func (s *kernel) processTemplate(ctx context.Context, id string) (ContentTemplate, bool, error) {
	// 读当前各份，空库首次才落预置。
	rows, err := s.ensureProcessTemplates(ctx)
	// 读不到就停。
	if err != nil {
		return ContentTemplate{}, false, err
	}
	// 逐份比身份，大小写不计。
	for _, t := range rows {
		// 对上就交回这一份。
		if strings.EqualFold(t.ID.String(), id) {
			return t, true, nil
		}
	}
	return ContentTemplate{}, false, nil
}

// parseProcessSchema 工艺模版必须是对象根，且过工艺模版的额外规则；空则空字段表。
func parseProcessSchema(id uuid.UUID, raw []byte) ([]byte, error) {
	// 空的按空字段表处理。
	if len(bytes.TrimSpace(raw)) == 0 {
		// 换成空对象字段表。
		raw = []byte(`{"root":"object"}`)
	}
	// 解析字段表，失败就不能继续。
	sch, err := contenttpl.Parse(raw)
	// 格式不对就拒绝。
	if err != nil {
		return nil, domain.ErrTemplateInvalid
	}
	// 保留键和预置包角的骨架在这里核。
	if err := contenttpl.ValidateProcessTemplate(id.String(), sch); err != nil {
		return nil, domain.ErrTemplateInvalid
	}
	// 编成规范字节。
	return contenttpl.Marshal(sch)
}

// ListProcessTemplates 列出当前各份工艺模版；只有 WAN 管理员能读。
func (s *Templates) ListProcessTemplates(ctx context.Context, token string) ([]ContentTemplate, error) {
	// 只有 WAN 管理员能读模版，失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续。
	if err != nil {
		// 读模版被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "get_template", KindProcess, audit.Deny)
		return nil, err
	}
	// 读当前各份，空库首次才落预置。
	rows, err := s.ensureProcessTemplates(ctx)
	// 读不到就记拒绝。
	if err != nil {
		// 读模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "get_template", KindProcess, audit.Deny)
		return nil, err
	}
	// 读取成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "get_template", KindProcess, audit.Allow); err != nil {
		return nil, err
	}
	return rows, nil
}

// CreateProcessTemplate 新建一份工艺模版并下发；带身份只许是预置身份，用来按原身份补建删掉的那份。
func (s *Templates) CreateProcessTemplate(ctx context.Context, token string, id uuid.UUID, name string, schemaJSON []byte) (ContentTemplate, error) {
	// 只有 WAN 管理员能改模版，失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "update_template", KindProcess, audit.Deny)
		return ContentTemplate{}, err
	}
	// 记拒绝的小工具，免得每个分支重复写。
	deny := func(target string, cause error) (ContentTemplate, error) {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", target, audit.Deny)
		return ContentTemplate{}, cause
	}
	// 带身份就是补建预置的那份。
	if id != uuid.Nil {
		// 只认预置身份，别的身份不许自己指定。
		seed, ok := contenttpl.SeedProcessTemplate(id.String())
		// 不是预置身份就拒绝。
		if !ok {
			// 记一笔拒绝审计再交回原因。
			return deny(KindProcess, domain.ErrTemplateInvalid)
		}
		// 这份还在就不能再建。
		_, err = s.store.TemplateByID(ctx, id)
		// 还在就拒绝。
		if err == nil {
			// 记一笔拒绝审计再交回原因。
			return deny(seed.Name, domain.ErrTemplateInvalid)
		}
		// 不是没有这条，就当真正的故障。
		if !errors.Is(err, domain.ErrNotFound) {
			// 记一笔拒绝审计再交回原因。
			return deny(seed.Name, err)
		}
		// 没给名称就用预置名称。
		if strings.TrimSpace(name) == "" {
			// 沿用预置名称。
			name = seed.Name
		}
		// 没给字段表就用预置字段表。
		if len(bytes.TrimSpace(schemaJSON)) == 0 {
			// 编出预置字段表。
			schemaJSON, err = contenttpl.Marshal(contenttpl.ObjectSchema(seed.Fields))
			// 编不出就拒绝。
			if err != nil {
				// 记一笔拒绝审计再交回原因。
				return deny(name, domain.ErrTemplateInvalid)
			}
		}
	}
	// 名称去空白，空或过长拒绝。
	name, err = normalizeProjectName(name)
	// 名称不合法就拒绝。
	if err != nil {
		// 记一笔拒绝审计再交回原因。
		return deny(KindProcess, err)
	}
	// 字段表收成规范形并过工艺模版规则。
	canon, err := parseProcessSchema(id, schemaJSON)
	// 字段表不合法就拒绝。
	if err != nil {
		// 记一笔拒绝审计再交回原因。
		return deny(name, err)
	}
	// 读当前各份，看有没有到上限。
	cur, err := s.ensureProcessTemplates(ctx)
	// 读不到就拒绝。
	if err != nil {
		// 记一笔拒绝审计再交回原因。
		return deny(name, err)
	}
	// 份数到上限就不再建。
	if len(cur) >= contenttpl.MaxProcessTemplates() {
		// 记一笔拒绝审计再交回原因。
		return deny(name, domain.ErrTemplateInvalid)
	}
	// 写入这一份；同名会被库拒绝。
	row, err := s.store.InsertTemplate(ctx, ContentTemplate{
		ID: id, Kind: KindProcess, Name: name, Schema: canon, Digest: digest.Sum(canon),
	})
	// 写入失败就记拒绝。
	if err != nil {
		// 记一笔拒绝审计再交回原因。
		return deny(name, err)
	}
	// 解开核对，坏的不下发。
	row, err = openTemplate(row)
	// 核不过就记拒绝。
	if err != nil {
		// 记一笔拒绝审计再交回原因。
		return deny(name, err)
	}
	// 新建成功才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", processTemplateTarget(row.Name, row.Revision), audit.Allow); err != nil {
		return ContentTemplate{}, err
	}
	// 通知厂端来拉这一份。
	s.notifyTemplate(ctx, row.ID, row.Kind, row.Revision)
	// 再推全集，厂端按它对账。
	s.notifyProcessTemplateSet(ctx)
	return row, nil
}

// UpdateProcessTemplate 按身份和期望修订改一份工艺模版的名称与字段表并下发；不改已有工艺正文。
func (s *Templates) UpdateProcessTemplate(ctx context.Context, token string, id uuid.UUID, expected int64, name string, schemaJSON []byte) (ContentTemplate, error) {
	// 只有 WAN 管理员能改模版，失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "update_template", KindProcess, audit.Deny)
		return ContentTemplate{}, err
	}
	// 记拒绝的小工具，免得每个分支重复写。
	deny := func(target string, cause error) (ContentTemplate, error) {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", target, audit.Deny)
		return ContentTemplate{}, cause
	}
	// 名称去空白，空或过长拒绝。
	name, err = normalizeProjectName(name)
	// 名称不合法就拒绝。
	if err != nil {
		// 记一笔拒绝审计再交回原因。
		return deny(KindProcess, err)
	}
	// 字段表收成规范形并过工艺模版规则。
	canon, err := parseProcessSchema(id, schemaJSON)
	// 字段表不合法就拒绝。
	if err != nil {
		// 记一笔拒绝审计再交回原因。
		return deny(name, err)
	}
	// 读这一份当前行。
	cur, err := s.store.TemplateByID(ctx, id)
	// 没有就拒绝。
	if err != nil {
		// 记一笔拒绝审计再交回原因。
		return deny(name, err)
	}
	// 身份属于工程模版就按不存在。
	if cur.Kind != KindProcess {
		// 记一笔拒绝审计再交回原因。
		return deny(name, domain.ErrNotFound)
	}
	// 解开核对当前这份。
	cur, err = openTemplate(cur)
	// 当前这份坏了就拒绝。
	if err != nil {
		// 记一笔拒绝审计再交回原因。
		return deny(name, err)
	}
	// 期望修订对不上就是别人改过。
	if expected != cur.Revision {
		// 记一笔拒绝审计再交回原因。
		return deny(processTemplateTarget(name, expected), domain.ErrRevisionConflict)
	}
	// 名称和字段表都没变就不升修订。
	if cur.Name == name && bytes.Equal(cur.Schema, canon) {
		// 照样记一笔允许。
		if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", processTemplateTarget(cur.Name, cur.Revision), audit.Allow); err != nil {
			return ContentTemplate{}, err
		}
		return cur, nil
	}
	// 按期望修订改库。
	row, err := s.store.UpdateTemplateByID(ctx, id, expected, name, canon, digest.Sum(canon))
	// 改不成就记拒绝。
	if err != nil {
		// 记一笔拒绝审计再交回原因。
		return deny(processTemplateTarget(name, expected), err)
	}
	// 解开核对新的一份。
	row, err = openTemplate(row)
	// 核不过就记拒绝。
	if err != nil {
		// 记一笔拒绝审计再交回原因。
		return deny(processTemplateTarget(name, expected), err)
	}
	// 改成才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", processTemplateTarget(row.Name, row.Revision), audit.Allow); err != nil {
		return ContentTemplate{}, err
	}
	// 通知厂端来拉新修订。
	s.notifyTemplate(ctx, row.ID, row.Kind, row.Revision)
	return row, nil
}

// DeleteProcessTemplate 删掉一份工艺模版；已有工艺正文不动，之后归到它的工艺不再套模版。
func (s *Templates) DeleteProcessTemplate(ctx context.Context, token string, id uuid.UUID) error {
	// 只有 WAN 管理员能改模版，失败一律记拒绝。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "update_template", KindProcess, audit.Deny)
		return err
	}
	// 读这一份当前行。
	cur, err := s.store.TemplateByID(ctx, id)
	// 没有就拒绝。
	if err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", KindProcess, audit.Deny)
		return err
	}
	// 身份属于工程模版就按不存在。
	if cur.Kind != KindProcess {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", KindProcess, audit.Deny)
		return domain.ErrNotFound
	}
	// 删行并记墓碑。
	if err := s.store.DeleteTemplate(ctx, KindProcess, id); err != nil {
		// 改模版被拒就留审计。
		_ = s.audit(ctx, &admin.ID, nil, nil, "update_template", processTemplateTarget(cur.Name, cur.Revision), audit.Deny)
		return err
	}
	// 删成才记允许。
	if err := s.audit(ctx, &admin.ID, nil, nil, "update_template", processTemplateTarget(cur.Name, cur.Revision), audit.Allow); err != nil {
		return err
	}
	// 把剩下的和全集再推一遍，厂端撤下已删的。
	s.notifyRemainingTemplates(ctx)
	return nil
}
