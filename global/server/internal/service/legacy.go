package service

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"wmesh/global/internal/platform/audit"
	"wmesh/global/internal/platform/contenttpl"
	"wmesh/global/internal/platform/digest"
	"wmesh/global/internal/platform/domain"
	"wmesh/global/internal/store"
)

var gapFolderRe = regexp.MustCompile(`^(\d+)H-(\d+(?:\.\d+)?)-(\d+(?:\.\d+)?)`) // 目录名：层H-最小-最大

// LegacyFile 是一次导入里的一份旧 JSON，路径相对工艺/工程根。
type LegacyFile struct {
	Path      string // 相对路径，如 Standard/1T1.2/1K.json
	Name      string // 给人看的名字；空则从路径或正文取
	Content   []byte // UTF-8 JSON
	Overwrite bool   // 本份同名改正文；可盖过整批
	Rename    bool   // 本份同名按路径另起；可盖过整批
}

// LegacyImport 一次性导入：先工艺后工程，收入平台级。
type LegacyImport struct {
	Processes []LegacyFile // 工艺文件，同一路径只入一次
	Projects  []LegacyFile // 工程文件；缺路径则该份拒绝
	Overwrite bool         // 同名改正文保留身份；否就跳过同名
	Rename    bool         // 同名按路径另起新名新建；覆盖优先
}

// LegacyReject 是未入库的一份工程或坏工艺，已入工艺仍保留。
type LegacyReject struct {
	Path   string `json:"path"`   // 相对路径
	Reason string `json:"reason"` // 英文原因，无正文
}

// LegacyImportResult 是本批导入结果；部分工程拒绝时其它份仍可成功。
type LegacyImportResult struct {
	Processes []Asset        `json:"processes"` // 新建或覆盖后的平台工艺
	Projects  []Asset        `json:"projects"`  // 新建或覆盖后的平台工程
	Rejected  []LegacyReject `json:"rejected"`  // 未入的工程或坏文件
	Skipped   []LegacyReject `json:"skipped"`   // 同名跳过，沿用已有身份
}

// ImportLegacy 由 WAN 管理员把旧文件收成平台级资产。
func (s *Assets) ImportLegacy(ctx context.Context, token string, in LegacyImport) (LegacyImportResult, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 导入被拒就留审计。
		_ = s.audit(ctx, nil, nil, nil, "import_legacy", "wan", audit.Deny)
		return LegacyImportResult{}, err
	}
	// 先准备导入结果，坏文件单独收集。
	out := LegacyImportResult{Processes: []Asset{}, Projects: []Asset{}, Rejected: []LegacyReject{}, Skipped: []LegacyReject{}}
	// 做完这一步再继续。
	names, err := s.platformImportNames(ctx)
	// 这一步失败就停，避免留下半截。
	if err != nil {
		return LegacyImportResult{}, err
	}
	// 准备一个新的空结构。
	idx := newPathIndex()
	// 用来挡住同一键被写两次。
	seen := map[string]struct{}{}
	// 逐个工艺文件导入，坏的记入拒绝。
	for _, f := range in.Processes {
		// 收成合法值，空或太长不要。
		rel := normalizeLegacyPath(f.Path)
		// 空和有值走不同路，避免把空白写进名录。
		if rel == "" {
			// 把这一项接进结果。
			out.Rejected = append(out.Rejected, LegacyReject{Path: f.Path, Reason: "invalid path"})
			continue
		}
		// 同一键已经见过则拒绝，防止写两遍。
		if _, ok := seen[rel]; ok {
			continue
		}
		// 这个相对路径已经导过，不能再导一次。
		seen[rel] = struct{}{}
		// 不是合法结构就拒绝，避免坏正文入库。
		if !json.Valid(f.Content) {
			// 把这一项接进结果。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: "invalid process json"})
			continue
		}
		// 去掉多余空白或前后缀。
		name := strings.TrimSpace(f.Name)
		// 空和有值走不同路，避免把空白写进名录。
		if name == "" {
			// 按工艺规则取名或核对。
			name = processDisplayName(f.Content, rel)
		}
		// 整理导入的名字或路径。
		overwrite, rename := importFileFlags(in, f)
		// 同名已经在库里，按覆盖或跳过处理。
		if cur, ok := names[importNameKey(KindProcess, name)]; ok {
			// 不允许覆盖时，同名就拒绝或改名再导入。
			if !overwrite {
				// 条件不满足则拒绝，避免把错状态写进去。
				if !rename {
					// 记进路径对照，失败就不能继续。
					idx.add(rel, cur.ID)
					// 把这一项接进结果。
					out.Skipped = append(out.Skipped, LegacyReject{Path: rel, Reason: "name exists"})
					continue
				}
				// 找一个还不冲突的名字。
				name = uniqueImportName(names, KindProcess, name, rel)
				// 前面不成立就走这里，避免两种结果混用。
			} else {
				// 按新内容覆盖，草稿顺手改可用。
				row, err := s.overwriteImported(ctx, token, cur, f.Content)
				// 覆盖失败就拒绝，不能留半新正文。
				if err != nil {
					// 把这一项接进结果。
					out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
					continue
				}
				// 刚写入的名字马上能被同名命中。
				rememberImportName(names, row)
				// 记进路径对照，失败就不能继续。
				idx.add(rel, row.ID)
				// 把这一项接进结果。
				out.Processes = append(out.Processes, row)
				continue
			}
		}
		// 新建这一条，已有或没资格则不行。
		row, err := s.CreatePlatformProcess(ctx, token, name, f.Content)
		// 新建失败就停，避免留下半截记录。
		if err != nil {
			// 把这一项接进结果。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		// 发布给该收到的一方。
		row, err = s.PublishPlatformAsset(ctx, token, row.ID, row.Revision)
		// 没就绪就拒绝，避免对方以为可用。
		if err != nil {
			// 把这一项接进结果。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		// 刚写入的名字马上能被同名命中。
		rememberImportName(names, row)
		// 记进路径对照，失败就不能继续。
		idx.add(rel, row.ID)
		// 把这一项接进结果。
		out.Processes = append(out.Processes, row)
	}
	// 按缝隙目录收打底和盖面。
	bands := idx.gapBands()
	// 逐个工程文件导入，依赖不齐就拒绝。
	for _, f := range in.Projects {
		// 收成合法值，空或太长不要。
		rel := normalizeLegacyPath(f.Path)
		// 去掉多余空白或前后缀。
		name := strings.TrimSpace(f.Name)
		// 空和有值走不同路，避免把空白写进名录。
		if name == "" {
			// 按工程规则取名或核对。
			name = projectDisplayName(rel)
		}
		// 整理导入的名字或路径。
		overwrite, rename := importFileFlags(in, f)
		// 按是不是工程决定要不要核对焊道和依赖。
		if _, ok := names[importNameKey(KindProject, name)]; ok && !overwrite {
			// 条件不满足则拒绝，避免把错状态写进去。
			if !rename {
				// 把这一项接进结果。
				out.Skipped = append(out.Skipped, LegacyReject{Path: rel, Reason: "name exists"})
				continue
			}
			// 找一个还不冲突的名字。
			name = uniqueImportName(names, KindProject, name, rel)
		}
		// 把旧身份改成新的。
		body, err := rewriteLegacyProject(f.Content, rel, idx, bands)
		// 改不完就拒绝，避免还指向旧的。
		if err != nil {
			// 把这一项接进结果。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		// 按是不是工程决定要不要核对焊道和依赖。
		if cur, ok := names[importNameKey(KindProject, name)]; ok {
			// 按新内容覆盖，草稿顺手改可用。
			row, err := s.overwriteImported(ctx, token, cur, body)
			// 覆盖失败就拒绝，不能留半新正文。
			if err != nil {
				// 把这一项接进结果。
				out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
				continue
			}
			// 刚写入的名字马上能被同名命中。
			rememberImportName(names, row)
			// 把这一项接进结果。
			out.Projects = append(out.Projects, row)
			continue
		}
		// 把导入结果落成平台级。
		row, err := s.insertImportedProject(ctx, token, name, body)
		// 落库失败就拒绝这个文件。
		if err != nil {
			// 把这一项接进结果。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		// 发布给该收到的一方。
		row, err = s.PublishPlatformAsset(ctx, token, row.ID, row.Revision)
		// 没就绪就拒绝，避免对方以为可用。
		if err != nil {
			// 把这一项接进结果。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		// 刚写入的名字马上能被同名命中。
		rememberImportName(names, row)
		// 把这一项接进结果。
		out.Projects = append(out.Projects, row)
	}
	// 先看数量，失败就不能继续。
	target := "processes=" + strconv.Itoa(len(out.Processes)) + " projects=" + strconv.Itoa(len(out.Projects)) + " rejected=" + strconv.Itoa(len(out.Rejected)) + " skipped=" + strconv.Itoa(len(out.Skipped))
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "import_legacy", target, audit.Allow); err != nil {
		return LegacyImportResult{}, err
	}
	return out, nil
}

// insertImportedProject 按改写后正文落平台级工程，不套模版，免把 T 排元素丢掉。
func (s *Assets) insertImportedProject(ctx context.Context, token, name string, content []byte) (Asset, error) {
	// 核对当前管理员会话。
	admin, err := s.RequireAdmin(ctx, token)
	// 无效就不能继续，不当已经登录。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, nil, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 整理导入的名字或路径。
	deps, err := s.importedProjectDeps(ctx, admin.ID, name, content)
	// 冲突或越界就拒绝这个文件。
	if err != nil {
		return Asset{}, err
	}
	// 写入这一条，失败就不能继续。
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: KindProject, Name: name, Status: AssetDraft, WeldKind: contenttpl.InferWeldKind(content),
		Content: content, Digest: digest.Sum(content), CreatorID: admin.ID, Deps: deps,
	})
	// 这一步失败就停，避免留下半截。
	if err != nil {
		// 建资产被拒就留审计，不写正文。
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 审计没记下则中止，不当这次已经成功。
	if err := s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	// 去掉正文再返回。
	return stripContent(row), nil
}

// overwriteImported 改正文（工程同时改依赖），身份不变；草稿顺带发布。
func (s *Assets) overwriteImported(ctx context.Context, token string, cur Asset, content []byte) (Asset, error) {
	// 工程才填依赖，工艺先留空。
	deps := []AssetDep{}
	// 按是不是工程决定要不要核对焊道和依赖。
	if cur.Kind == KindProject {
		// 整理导入的名字或路径。
		d, err := s.importedProjectDeps(ctx, uuid.Nil, cur.Name, content)
		// 冲突或越界就拒绝这个文件。
		if err != nil {
			return Asset{}, err
		}
		// 工程改用按正文钉好的依赖。
		deps = d
	}
	// 按当前行改写；修订不符就拒绝。
	row, err := s.mutatePlatform(ctx, token, cur.ID, cur.Revision, "update_asset", func(live Asset) (store.AssetWrite, error) {
		// 已停用则拒绝变更，正文和依赖都锁住。
		if live.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		// 按是不是工程决定要不要核对焊道和依赖。
		if live.Kind == KindProject {
			// 对不上就拒绝，避免焊道和模式错配。
			if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
				return store.AssetWrite{}, err
			}
		}
		// 按内容算摘要，失败就不能继续。
		return store.AssetWrite{Name: live.Name, Content: content, Digest: digest.Sum(content), Copyable: live.Copyable, Status: live.Status, Deps: deps}, nil
	})
	// 这一步失败就停，避免留下半截。
	if err != nil {
		return Asset{}, err
	}
	// 还是草稿就接着发布，不能当已经可用。
	if row.Status == AssetDraft {
		// 发布给该收到的一方。
		return s.PublishPlatformAsset(ctx, token, row.ID, row.Revision)
	}
	return row, nil
}

// importedProjectDeps 按改写后正文钉当前可用平台工艺，不套模版。
func (s *Assets) importedProjectDeps(ctx context.Context, adminID uuid.UUID, name string, content []byte) ([]AssetDep, error) {
	// 把要的身份收集出来。
	ids, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), content)
	// 出现路径键就直接拒绝。
	if err != nil {
		// 有操作者才补记拒绝，系统任务可以没有人。
		if adminID != uuid.Nil {
			// 建资产被拒就留审计，不写正文。
			_ = s.audit(ctx, &adminID, nil, nil, "create_asset", name, audit.Deny)
		}
		return nil, domain.ErrAssetDependency
	}
	// 按身份钉住工艺，缺了或不可用就失败。
	deps, err := mergeProjectDeps(nil, ids, func(id uuid.UUID) (AssetDep, error) {
		// 装入并核对摘要。
		p, err := s.loadChecked(ctx, id)
		// 不符就不能把这条拿去用。
		if err != nil {
			// 没有这条就按不存在处理，不当成别的故障。
			if errors.Is(err, domain.ErrNotFound) {
				return AssetDep{}, domain.ErrAssetDependency
			}
			return AssetDep{}, err
		}
		// 不是工艺就拒绝，这项只对工艺开放。
		if p.Kind != KindProcess {
			return AssetDep{}, domain.ErrAssetDependency
		}
		// 不是可用就拒绝，草稿和停用不能当发布。
		if p.Status != AssetAvailable {
			return AssetDep{}, domain.ErrAssetNotAvailable
		}
		return AssetDep{ID: p.ID, Revision: p.Revision, Digest: p.Digest}, nil
	})
	// 这一步失败就停，避免留下半截。
	if err != nil {
		// 有操作者才补记拒绝，系统任务可以没有人。
		if adminID != uuid.Nil {
			// 建资产被拒就留审计，不写正文。
			_ = s.audit(ctx, &adminID, nil, nil, "create_asset", name, audit.Deny)
		}
		return nil, err
	}
	// 对不上就拒绝，避免焊道和模式错配。
	if err := s.assertPlatformProcessDeps(ctx, deps); err != nil {
		// 有操作者才补记拒绝，系统任务可以没有人。
		if adminID != uuid.Nil {
			// 建资产被拒就留审计，不写正文。
			_ = s.audit(ctx, &adminID, nil, nil, "create_asset", name, audit.Deny)
		}
		return nil, err
	}
	// 只允许这些身份，正文里多出来的拒绝。
	allowed := map[string]struct{}{}
	// 逐条核对依赖，缺了或类型不同就拒绝。
	for _, d := range deps {
		// 收成文本给审计或指令用。
		allowed[d.ID.String()] = struct{}{}
	}
	// 逐个身份处理，缺了就整次失败。
	for _, id := range ids {
		// 对不上就跳过或拒绝，避免用错那一条。
		if _, ok := allowed[id]; !ok {
			// 有操作者才补记拒绝，系统任务可以没有人。
			if adminID != uuid.Nil {
				// 建资产被拒就留审计，不写正文。
				_ = s.audit(ctx, &adminID, nil, nil, "create_asset", name, audit.Deny)
			}
			return nil, domain.ErrAssetDependency
		}
	}
	return deps, nil
}

// platformImportNames 未停用平台级按种类+显示名取最新一条。
func (s *Assets) platformImportNames(ctx context.Context) (map[string]Asset, error) {
	// 列出这一批供后面筛选。
	rows, err := s.store.ListAssets(ctx)
	// 列出失败就拒绝，避免交出不完整结果。
	if err != nil {
		return nil, err
	}
	// 按种类加名字索引，同名才能覆盖。
	out := map[string]Asset{}
	// 逐行整理，坏的一行就整批拒绝。
	for _, a := range rows {
		// 已停用则拒绝变更，正文和依赖都锁住。
		if a.Status == AssetDisabled {
			continue
		}
		// 整理导入的名字或路径。
		k := importNameKey(a.Kind, a.Name)
		// 对不上就跳过或拒绝，避免用错那一条。
		if _, ok := out[k]; ok {
			continue
		}
		// 同名只留先遇到的，避免后面的盖掉。
		out[k] = a
	}
	return out, nil
}

// importNameKey 同级同名对照键。
func importNameKey(kind, name string) string {
	return kind + "\n" + name
}

// rememberImportName 本批新建或覆盖后立刻可被后续同名命中。
func rememberImportName(names map[string]Asset, row Asset) {
	// 整理导入的名字或路径。
	names[importNameKey(row.Kind, row.Name)] = row
}

// 导入路径到工艺身份的对照，不含正文。
type pathIndex struct {
	byPath map[string]uuid.UUID // 相对路径对应到工艺身份。
	files  []legacyMapped       // 原始相对路径，用来收间隙带，不含别名
}

// 一条已入库工艺及其原始相对路径。
type legacyMapped struct {
	Path string    // 导入时的相对路径
	ID   uuid.UUID // 入库后的工艺身份
}

// newPathIndex 空对照表。
func newPathIndex() *pathIndex {
	return &pathIndex{byPath: map[string]uuid.UUID{}}
}

// add 记下相对路径及其不含 processes/ 前缀的别名。
func (idx *pathIndex) add(rel string, id uuid.UUID) {
	// 把这一项接进结果。
	idx.files = append(idx.files, legacyMapped{Path: rel, ID: id})
	// 原相对路径对到这条工艺。
	idx.byPath[rel] = id
	// 去掉多余空白或前后缀。
	trimmed := strings.TrimPrefix(rel, "processes/")
	// 去掉固定目录前缀后也能对上。
	idx.byPath[trimmed] = id
	// 条件不满足则拒绝，避免把错状态写进去。
	if !strings.Contains(trimmed, "/") {
		// 取最后一段名字。
		idx.byPath[path.Base(rel)] = id
	}
}

// lookup 空路径算未选；对不上则失败。
func (idx *pathIndex) lookup(p string) (uuid.UUID, bool) {
	// 收成合法值，空或太长不要。
	p = normalizeLegacyPath(p)
	// 空和有值走不同路，避免把空白写进名录。
	if p == "" {
		return uuid.Nil, true
	}
	// 对不上就跳过或拒绝，避免用错那一条。
	if id, ok := idx.byPath[p]; ok {
		return id, true
	}
	// 对不上就跳过或拒绝，避免用错那一条。
	if id, ok := idx.byPath[strings.TrimPrefix(p, "processes/")]; ok {
		return id, true
	}
	// 对不上就跳过或拒绝，避免用错那一条。
	if id, ok := idx.byPath["processes/"+p]; ok {
		return id, true
	}
	return uuid.Nil, false
}

// gapBands 按缝隙目录收打底/盖面引用；缺文件的带仍可留下空引用。
func (idx *pathIndex) gapBands() []any {
	// 就地记下一条缝的名字、层和打底盖面。
	type folder struct {
		name      string    // 缝隙目录的显示名。
		layer     int       // 层号，用来排缝隙顺序。
		min, max  float64   // 间隙的下限和上限。
		root, cap uuid.UUID // 打底和盖面的工艺身份。
	}
	// 按目录收缝隙带，同一目录合成一条。
	folders := map[string]*folder{}
	// 逐个已入库文件，用来收缝隙带。
	for _, item := range idx.files {
		// 取出导入时的相对路径，用来认缝隙目录。
		p := item.Path
		// 取出入库后的工艺身份，打底盖面要写它。
		id := item.ID
		// 做完这一步再继续。
		dir, file := path.Split(strings.TrimSuffix(p, "/"))
		// 去掉多余空白或前后缀。
		dir = strings.TrimSuffix(dir, "/")
		// 取最后一段名字。
		base := path.Base(dir)
		// 按目录名抽出层和间隙。
		m := gapFolderRe.FindStringSubmatch(base)
		// 还没有这条记录就新建或跳过，不能当已有。
		if m == nil {
			continue
		}
		// 条件不满足则拒绝，避免把错状态写进去。
		if !strings.Contains(dir, "T排立对接") && !strings.Contains(strings.ToLower(dir), "tbar") {
			// 条件不满足则拒绝，避免把错状态写进去。
			if !strings.Contains(dir, "6T1.2") {
				continue
			}
		}
		// 把层号收成整数。
		layer, _ := strconv.Atoi(m[1])
		// 把间隙收成数字。
		min, _ := strconv.ParseFloat(m[2], 64)
		// 把间隙收成数字。
		max, _ := strconv.ParseFloat(m[3], 64)
		// 取出已有的缝隙带，没有就新建。
		f := folders[dir]
		// 还没有这条记录就新建或跳过，不能当已有。
		if f == nil {
			// 新建这条缝隙带，打底盖面后面再填。
			f = &folder{name: base, layer: layer, min: min, max: max}
			// 这个缝隙目录归到同一条带里。
			folders[dir] = f
		}
		// 去掉多余空白或前后缀。
		name := strings.TrimSuffix(file, path.Ext(file))
		// 按下面第一条成立的条件分叉。
		switch {
		// 文件名含打底，记成这条缝的根部。
		case strings.Contains(name, "打底"):
			// 文件名是打底，记成这条缝的根部。
			f.root = id
		// 文件名含盖面，记成这条缝的盖面。
		case strings.Contains(name, "盖面"):
			// 文件名是盖面，记成这条缝的盖面。
			f.cap = id
		}
	}
	// 只收集有打底或盖面的缝隙带。
	var list []*folder
	// 逐个目录收口，缺文件的带可以留空。
	for _, f := range folders {
		// 身份是空就按未设置处理，避免写空号。
		if f.root == uuid.Nil && f.cap == uuid.Nil {
			continue
		}
		// 把这一项接进结果。
		list = append(list, f)
	}
	// 按层号和间隙下限排序，顺序才稳定。
	sort.Slice(list, func(i, j int) bool {
		// 条件不满足则拒绝，避免把错状态写进去。
		if list[i].layer != list[j].layer {
			return list[i].layer < list[j].layer
		}
		return list[i].min < list[j].min
	})
	// 按数量先准备容器。
	out := make([]any, 0, len(list))
	// 按排好的顺序收成结果，空的不再留下。
	for _, f := range list {
		// 收成浮点数，失败就不能继续。
		band := map[string]any{"minGap": f.min, "maxGap": f.max, "layer": float64(f.layer), "rootProcessId": "", "capProcessId": ""}
		// 身份是空就按未设置处理，避免写空号。
		if f.root != uuid.Nil {
			// 收成文本给审计或指令用。
			band["rootProcessId"] = f.root.String()
		}
		// 身份是空就按未设置处理，避免写空号。
		if f.cap != uuid.Nil {
			// 收成文本给审计或指令用。
			band["capProcessId"] = f.cap.String()
		}
		// 把这一项接进结果。
		out = append(out, band)
	}
	return out
}

// rewriteLegacyProject 把 processPath 换成身份，删掉内嵌工艺对象，并打上模版身份。
func rewriteLegacyProject(raw []byte, filePath string, idx *pathIndex, bands []any) ([]byte, error) {
	// 先接住任意节点，再按类型往下拆。
	var v any
	// 载荷解开失败则拒绝，不按坏包继续。
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, domain.ErrForbidden
	}
	// 做完这一步再继续。
	arr, ok := v.([]any)
	// 对不上就跳过或拒绝，避免用错那一条。
	if !ok {
		return nil, domain.ErrForbidden
	}
	// 按工程规则取名或核对。
	fileKind := projectFileKind(filePath)
	// 改不完就拒绝，避免还指向旧的。
	if err := rewriteRefs(arr, idx); err != nil {
		return nil, err
	}
	// 逐个元素往下拆，遇到路径键要拒绝。
	for _, el := range arr {
		// 做完这一步再继续。
		obj, _ := el.(map[string]any)
		// 还没有准备好就停，避免空着往下用。
		if obj == nil {
			continue
		}
		// 沿用文件判断出的种类，不重复猜。
		kind := fileKind
		// 空和有值走不同路，避免把空白写进名录。
		if kind == "" {
			// 从名字推断种类。
			kind = contenttpl.InferItemKind(obj)
		}
		// 盖上种类和模版身份。
		stampTemplate(obj, kind, bands)
	}
	// 编成字节再送出。
	out, err := json.Marshal(arr)
	// 编不出就拒绝，不发送半截。
	if err != nil {
		return nil, err
	}
	return out, nil
}

// stampTemplate 按种类钉空库模版身份；T 排补上当时间隙带。
func stampTemplate(obj map[string]any, kind string, bands []any) {
	// 按工艺、工程或软件种类走不同路。
	switch kind {
	// 多层项按多层模版收口。
	case contenttpl.ItemMulti:
		// 多层工程标上多层模版，厂端才认。
		obj["templateId"] = contenttpl.SeedTplMulti
		// 这条标成多层，避免按单层去解。
		obj["kind"] = contenttpl.ItemMulti
	// T 排项按 T 排模版收口。
	case contenttpl.ItemTBar:
		// T 排工程标上 T 排模版。
		obj["templateId"] = contenttpl.SeedTplTBar
		// 这条标成 T 排，避免和多层混。
		obj["kind"] = contenttpl.ItemTBar
		// 对不上就跳过或拒绝，避免用错那一条。
		if _, ok := obj["gapBands"]; !ok {
			// 把缝隙带写进工程，缺文件的可以是空。
			obj["gapBands"] = bands
		}
	// 其余情况走这里，避免漏掉一种状态。
	default:
		// 单层工程标上单层模版。
		obj["templateId"] = contenttpl.SeedTplSingle
		// 这条标成单层，避免按多层去解。
		obj["kind"] = contenttpl.ItemSingle
	}
}

// rewriteRefs 非空 processPath 必须能对上已入工艺，否则整份工程拒绝。
func rewriteRefs(v any, idx *pathIndex) error {
	// 按节点类型往下拆，别的类型忽略。
	switch x := v.(type) {
	// 数组继续往下拆，里面可能是缝隙或焊道。
	case []any:
		// 逐个字段往下看，不认识的先留着。
		for _, el := range x {
			// 改不完就拒绝，避免还指向旧的。
			if err := rewriteRefs(el, idx); err != nil {
				return err
			}
		}
	// 对象按字段往下拆，路径键要拒绝。
	case map[string]any:
		// 正文里出现路径键则拒绝，必须改用身份。
		if raw, ok := x["processPath"]; ok {
			// 做完这一步再继续。
			s, _ := raw.(string)
			// 按路径找回身份。
			id, found := idx.lookup(s)
			// 条件不满足则拒绝，避免把错状态写进去。
			if !found {
				return domain.ErrAssetDependency
			}
			// 去掉这个键，避免以后还命中。
			delete(x, "processPath")
			// 空和有值走不同路，避免把空白写进名录。
			if s == "" || id == uuid.Nil {
				// 对不上工艺就清空引用，不能留旧路径。
				x["processId"] = ""
			} else {
				// 收成文本给审计或指令用。
				x["processId"] = id.String()
			}
		}
		// 对不上就跳过或拒绝，避免用错那一条。
		if proc, ok := x["process"]; ok {
			// 条件不满足则拒绝，避免把错状态写进去。
			if _, isObj := proc.(map[string]any); isObj {
				// 去掉这个键，避免以后还命中。
				delete(x, "process")
			}
		}
		// 逐个字段下钻，路径引用要改成身份。
		for k, child := range x {
			// 条件不满足则拒绝，避免把错状态写进去。
			if k == "processId" {
				continue
			}
			// 改不完就拒绝，避免还指向旧的。
			if err := rewriteRefs(child, idx); err != nil {
				return err
			}
		}
	}
	return nil
}

// projectFileKind 按相对路径判断整份种类；空则逐条推断。
func projectFileKind(p string) string {
	// 做完这一步再继续。
	n := strings.ToLower(normalizeLegacyPath(p))
	// 条件不满足则拒绝，避免把错状态写进去。
	if strings.HasSuffix(n, "multilayer_data.json") || strings.Contains(n, "/multi_layer/") || strings.Contains(n, "/multilayer/") {
		return contenttpl.ItemMulti
	}
	// 条件不满足则拒绝，避免把错状态写进去。
	if strings.Contains(n, "/tbar/") || strings.HasPrefix(n, "tbar/") {
		return contenttpl.ItemTBar
	}
	return ""
}

// normalizeLegacyPath 统一斜杠并去掉无意义的前缀。
func normalizeLegacyPath(p string) string {
	// 去掉多余空白或前后缀。
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	// 去掉多余空白或前后缀。
	p = strings.TrimPrefix(p, "./")
	// 整理路径，失败就不能继续。
	p = path.Clean(p)
	// 条件不满足则拒绝，避免把错状态写进去。
	if p == "." {
		return ""
	}
	// 去掉多余空白或前后缀。
	return strings.TrimPrefix(p, "/")
}

// processDisplayName 优先用工艺 JSON 的 name，否则用文件名。
func processDisplayName(body []byte, rel string) string {
	// 先留空，解开成对象后再改字段。
	var obj map[string]any
	// 能解开才按对象改，坏包走别的拒绝。
	if json.Unmarshal(body, &obj) == nil {
		// 空和有值走不同路，避免把空白写进名录。
		if n, _ := obj["name"].(string); strings.TrimSpace(n) != "" {
			// 去掉多余空白或前后缀。
			return strings.TrimSpace(n)
		}
	}
	// 取最后一段名字。
	base := path.Base(rel)
	// 去掉多余空白或前后缀。
	return strings.TrimSuffix(base, path.Ext(base))
}

// projectDisplayName 用工程所在文件夹名。
func projectDisplayName(rel string) string {
	// 取所在目录，失败就不能继续。
	dir := path.Dir(normalizeLegacyPath(rel))
	// 空和有值走不同路，避免把空白写进名录。
	if base := path.Base(dir); base != "." && base != "/" && base != "" {
		return base
	}
	return "imported-project"
}

// importFileFlags 单份覆盖/重命名可盖过整批；覆盖仍优先。
func importFileFlags(in LegacyImport, f LegacyFile) (overwrite, rename bool) {
	return in.Overwrite || f.Overwrite, in.Rename || f.Rename
}

// uniqueImportName 同名按相对路径另起；仍撞则加序号。
func uniqueImportName(names map[string]Asset, kind, name, rel string) string {
	// 去掉多余空白或前后缀。
	label := strings.TrimSpace(legacyPathLabel(rel))
	// 空和有值走不同路，避免把空白写进名录。
	if label != "" && label != name {
		// 同名已经在库里，按覆盖或跳过处理。
		if _, ok := names[importNameKey(kind, label)]; !ok {
			return label
		}
		// 改用路径标签当名字，避开已经占用的原名。
		name = label
	}
	// 从二开始加序号，直到这个名字还没占用。
	for i := 2; i < 1000; i++ {
		// 把序号收成文本。
		n := name + " (" + strconv.Itoa(i) + ")"
		if _, ok := names[importNameKey(kind, n)]; !ok {
			return n
		}
	}
	return name
}

// legacyPathLabel 用去掉后缀的相对路径当新名；多层工程另标一层。
func legacyPathLabel(rel string) string {
	// 收成合法值，空或太长不要。
	rel = normalizeLegacyPath(rel)
	// 空和有值走不同路，避免把空白写进名录。
	if rel == "" {
		return ""
	}
	// 去掉多余空白或前后缀。
	stem := strings.TrimSuffix(rel, path.Ext(rel))
	// 取最后一段名字。
	base := path.Base(stem)
	// 条件不满足则拒绝，避免把错状态写进去。
	if base != "project_data" && base != "multilayer_data" {
		return stem
	}
	// 取所在目录，失败就不能继续。
	dir := path.Dir(stem)
	// 条件不满足则拒绝，避免把错状态写进去。
	if dir == "." || dir == "/" {
		// 没有父目录时就用文件名本身当工程名。
		dir = base
	}
	// 条件不满足则拒绝，避免把错状态写进去。
	if base == "multilayer_data" {
		return dir + "-多层"
	}
	return dir
}
