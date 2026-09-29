package service

import (
	"context"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/audit"
	"wmesh/factory/internal/platform/contenttpl"
	"wmesh/factory/internal/platform/digest"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/store"
)

var gapFolderRe = regexp.MustCompile(`^(\d+)H-(\d+(?:\.\d+)?)-(\d+(?:\.\d+)?)`) // 目录名：层H-最小-最大

// LegacyFile 是一次导入里的一份旧 JSON，路径相对工艺/工程根。
type LegacyFile struct {
	Path      string // 相对路径，如 Standard/1T1.2/1K.json 或 tbar/x/project_data.json
	Name      string // 给人看的名字；空则从路径或正文取
	Content   []byte // UTF-8 JSON
	Overwrite bool   // 本份同名改正文；可盖过整批
	Rename    bool   // 本份同名按路径另起；可盖过整批
}

// LegacyImport 厂端一次性导入：先工艺后工程。
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
	Processes []Asset        `json:"processes"` // 新建或覆盖后的本厂工艺
	Projects  []Asset        `json:"projects"`  // 新建或覆盖后的本厂工程
	Rejected  []LegacyReject `json:"rejected"`  // 未入的工程或坏文件
	Skipped   []LegacyReject `json:"skipped"`   // 同名跳过，沿用已有身份
}

// ImportLegacy 由工厂超管或整厂管理员把旧文件收成厂级资产。
func (s *Assets) ImportLegacy(ctx context.Context, token string, in LegacyImport) (LegacyImportResult, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return LegacyImportResult{}, err
	}
	// 管理员以上：工厂超管或整厂管理员。操作员不行。
	if err := s.can(ctx, acc, permManageOrg, nil); err != nil {
		// 不是管理员则记拒绝，操作员不能导旧文件。
		_ = s.audit(ctx, &acc.ID, nil, "import_legacy", "factory", audit.Deny)
		return LegacyImportResult{}, err
	}
	// 先放空结果，工艺和工程分开，失败的另记。
	out := LegacyImportResult{
		Processes: []Asset{},
		Projects:  []Asset{},
		Rejected:  []LegacyReject{},
		Skipped:   []LegacyReject{},
	}
	// 取出未停用厂级的同名对照，个人级不参与。
	names, err := s.factoryImportNames(ctx)
	// 对照表读失败则整批导入停下。
	if err != nil {
		return LegacyImportResult{}, err
	}
	// 准备路径对照，工程靠它把旧路径换成身份。
	idx := newPathIndex()
	// 导入按直属工厂，不挂到某个组织节点。
	wc := WorkContext{Direct: true}
	// 同一相对路径只收一次，重复的丢掉。
	seen := map[string]struct{}{}
	// 先收工艺，后面工程才能把路径换成身份。
	for _, f := range in.Processes {
		// 统一斜杠并去掉无意义前缀，空路径算无效。
		rel := normalizeLegacyPath(f.Path)
		// 路径空则这一份不入库，其它份继续。
		if rel == "" {
			// 这一份没入库，记入未入清单，其它份继续。
			out.Rejected = append(out.Rejected, LegacyReject{Path: f.Path, Reason: "invalid path"})
			continue
		}
		// 同一路径本批已经收过，后面的丢掉。
		if _, ok := seen[rel]; ok {
			continue
		}
		// 记下这份路径，避免同一文件入两次。
		seen[rel] = struct{}{}
		// 工艺正文不是合法内容则这一份拒绝。
		if !json.Valid(f.Content) {
			// 这一份没入库，记入未入清单，其它份继续。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: "invalid process json"})
			continue
		}
		// 先取文件里的名字，空的再从路径推断。
		name := strings.TrimSpace(f.Name)
		// 名字空就从正文或路径取一个显示名。
		if name == "" {
			// 名字空就从正文或文件名取一个显示名。
			name = processDisplayName(f.Content, rel)
		}
		// 单份的覆盖或另起名可以盖过整批开关。
		overwrite, rename := importFileFlags(in, f)
		// 已有同名未停用工艺，再看覆盖还是另起名。
		if cur, ok := names[importNameKey(KindProcess, name)]; ok {
			// 不覆盖则改看是否按路径另起新名。
			if !overwrite {
				// 不另起名就跳过同名，沿用已有那一条。
				if !rename {
					// 记下原始路径和别名，后面工程才能对上。
					idx.add(rel, cur.ID)
					// 同名且不覆盖就跳过，沿用已有身份。
					out.Skipped = append(out.Skipped, LegacyReject{Path: rel, Reason: "name exists"})
					continue
				}
				// 同名按路径另起，仍撞就加序号。
				name = uniqueImportName(names, KindProcess, name, rel)
				// 允许覆盖时改正文，身份保持不变。
			} else {
				// 同名且允许覆盖时改正文，身份保持不变。
				row, err := s.overwriteImported(ctx, acc, token, cur, f.Content)
				// 这一份失败就记入未入清单，其它份继续。
				if err != nil {
					// 这一份没入库，记入未入清单，其它份继续。
					out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
					continue
				}
				// 本批刚入库的名字立刻参与后面的同名判断。
				rememberImportName(names, row)
				// 记下原始路径和别名，后面工程才能对上。
				idx.add(rel, row.ID)
				// 这份工艺已经入库，放进本批成功结果。
				out.Processes = append(out.Processes, row)
				continue
			}
		}
		// 把这份旧工艺收成厂级草稿。
		row, err := s.CreateFactoryProcess(ctx, token, wc, name, f.Content)
		// 这一份失败就记入未入清单，其它份继续。
		if err != nil {
			// 这一份没入库，记入未入清单，其它份继续。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		// 导入的草稿立刻发布成可用。
		row, err = s.PublishAsset(ctx, token, row.ID, row.Revision)
		// 这一份失败就记入未入清单，其它份继续。
		if err != nil {
			// 这一份没入库，记入未入清单，其它份继续。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		// 本批刚入库的名字立刻参与后面的同名判断。
		rememberImportName(names, row)
		// 记下原始路径和别名，后面工程才能对上。
		idx.add(rel, row.ID)
		// 这份工艺已经入库，放进本批成功结果。
		out.Processes = append(out.Processes, row)
	}
	// 按缝隙目录收打底和盖面，缺文件的带也可以留。
	bands := idx.gapBands()
	// 工艺入库后再收工程，失败只拒这一份。
	for _, f := range in.Projects {
		// 统一斜杠并去掉无意义前缀，空路径算无效。
		rel := normalizeLegacyPath(f.Path)
		// 先取文件里的名字，空的再从路径推断。
		name := strings.TrimSpace(f.Name)
		// 名字空就从正文或路径取一个显示名。
		if name == "" {
			// 工程用所在文件夹名当显示名。
			name = projectDisplayName(rel)
		}
		// 单份的覆盖或另起名可以盖过整批开关。
		overwrite, rename := importFileFlags(in, f)
		// 已有同名工程且不覆盖，再看是否另起名。
		if _, ok := names[importNameKey(KindProject, name)]; ok && !overwrite {
			// 不另起名就跳过同名，沿用已有那一条。
			if !rename {
				// 同名且不覆盖就跳过，沿用已有身份。
				out.Skipped = append(out.Skipped, LegacyReject{Path: rel, Reason: "name exists"})
				continue
			}
			// 同名按路径另起，仍撞就加序号。
			name = uniqueImportName(names, KindProject, name, rel)
		}
		// 把旧路径换成身份并打上模版，失败则整份不入。
		body, err := rewriteLegacyProject(f.Content, rel, idx, bands)
		// 这一份失败就记入未入清单，其它份继续。
		if err != nil {
			// 这一份没入库，记入未入清单，其它份继续。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		// 覆盖时改正文，工程身份保持不变。
		if cur, ok := names[importNameKey(KindProject, name)]; ok {
			// 同名且允许覆盖时改正文，身份保持不变。
			row, err := s.overwriteImported(ctx, acc, token, cur, body)
			// 这一份失败就记入未入清单，其它份继续。
			if err != nil {
				// 这一份没入库，记入未入清单，其它份继续。
				out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
				continue
			}
			// 本批刚入库的名字立刻参与后面的同名判断。
			rememberImportName(names, row)
			// 这份工程已经入库，放进本批成功结果。
			out.Projects = append(out.Projects, row)
			continue
		}
		// 按改写后的正文落厂级工程，不套会丢元素的模版。
		row, err := s.insertImportedProject(ctx, acc, wc, name, body)
		// 这一份失败就记入未入清单，其它份继续。
		if err != nil {
			// 这一份没入库，记入未入清单，其它份继续。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		// 导入的草稿立刻发布成可用。
		row, err = s.PublishAsset(ctx, token, row.ID, row.Revision)
		// 这一份失败就记入未入清单，其它份继续。
		if err != nil {
			// 这一份没入库，记入未入清单，其它份继续。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		// 本批刚入库的名字立刻参与后面的同名判断。
		rememberImportName(names, row)
		// 这份工程已经入库，放进本批成功结果。
		out.Projects = append(out.Projects, row)
	}
	// 把成功、未入和跳过的份数写进审计。
	target := "processes=" + strconv.Itoa(len(out.Processes)) + " projects=" + strconv.Itoa(len(out.Projects)) + " rejected=" + strconv.Itoa(len(out.Rejected)) + " skipped=" + strconv.Itoa(len(out.Skipped))
	// 成功必须记上审计，没记上则本次不算做成。
	if err := s.audit(ctx, &acc.ID, nil, "import_legacy", target, audit.Allow); err != nil {
		return LegacyImportResult{}, err
	}
	return out, nil
}

// insertImportedProject 按改写后正文落厂级工程，不套已收模版，免把 T 排元素丢掉。
func (s *Assets) insertImportedProject(ctx context.Context, acc Account, wc WorkContext, name string, content []byte) (Asset, error) {
	// 定下制作时的工作位置，并准备冻结路径。
	unitID, path, err := s.resolveAuthorContext(ctx, acc, wc)
	// 工作位置不合法则拒绝，并补记审计。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	// 按改写后的正文钉当前可用工艺。
	deps, err := s.importedProjectDeps(ctx, content)
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny, unitID, path)
		return Asset{}, err
	}
	// 校验都过了才落成一条资产。
	return s.insertGoverned(ctx, acc, unitID, path, KindProject, AssetLevelFactory, name, content, deps, true)
}

// overwriteImported 改正文（工程同时改依赖），身份不变；草稿顺带发布。
func (s *Assets) overwriteImported(ctx context.Context, acc Account, token string, cur Asset, content []byte) (Asset, error) {
	// 工艺覆盖不改依赖；工程在下面重钉。
	deps := []AssetDep{}
	// 工程覆盖时同时改依赖，工艺只改正文。
	if cur.Kind == KindProject {
		// 按改写后的正文钉当前可用工艺。
		d, err := s.importedProjectDeps(ctx, content)
		// 依赖钉不住则这份工程不能覆盖或新建。
		if err != nil {
			return Asset{}, err
		}
		// 工程用新钉的依赖替换旧的。
		deps = d
	}
	// 改正文并重算摘要；停用拒绝，工程同时换依赖。
	row, err := s.mutateAsset(ctx, acc, cur.ID, cur.Revision, "update_asset", func(live Asset) (store.AssetWrite, error) {
		// 停用的不能被导入覆盖。
		if live.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		// 工程还要确认引用仍落在新依赖里。
		if live.Kind == KindProject {
			// 有引用对不上依赖则拒绝保存。
			if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
				return store.AssetWrite{}, err
			}
		}
		// 拼出本次要写入的内容，没通过的字段不改。
		return store.AssetWrite{Name: live.Name, Content: content, Digest: digest.Sum(content), Copyable: live.Copyable, Status: live.Status, Deps: deps}, nil
	})
	// 这一步失败则停下，避免带着残缺结果继续。
	if err != nil {
		return Asset{}, err
	}
	// 盖完仍是草稿就顺手发布成可用。
	if row.Status == AssetDraft {
		// 导入的草稿立刻发布成可用。
		return s.PublishAsset(ctx, token, row.ID, row.Revision)
	}
	return row, nil
}

// importedProjectDeps 按改写后正文钉当前可用工艺，不套模版。
func (s *Assets) importedProjectDeps(ctx context.Context, content []byte) ([]AssetDep, error) {
	// 从改写后的正文收集工艺引用。
	ids, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), content)
	// 引用收集失败则整份依赖不成立。
	if err != nil {
		return nil, domain.ErrAssetDependency
	}
	// 按本厂或已收的可用工艺钉死当前修订。
	deps, err := mergeProjectDeps(nil, ids, func(id uuid.UUID) (AssetDep, error) {
		// 把引用钉到当前可用的厂级或已收工艺。
		return s.pinFactoryProcess(ctx, id)
	})
	// 钉不住则这条依赖不成立。
	if err != nil {
		return nil, err
	}
	// 依赖不合格则拒绝保存。
	if err := s.assertFactoryProcessDeps(ctx, deps); err != nil {
		return nil, err
	}
	// 把已钉上的身份收成集合，用来核正文引用。
	allowed := map[string]struct{}{}
	// 已钉上的身份收成允许集合。
	for _, d := range deps {
		// 记下已声明依赖的身份，用来核对正文引用。
		allowed[d.ID.String()] = struct{}{}
	}
	// 正文引用必须都能钉上，缺一条就拒绝。
	for _, id := range ids {
		// 有引用没钉上则整份工程依赖不成立。
		if _, ok := allowed[id]; !ok {
			return nil, domain.ErrAssetDependency
		}
	}
	return deps, nil
}

// factoryImportNames 未停用厂级按种类+显示名取最新一条，个人级不参与。
func (s *Assets) factoryImportNames(ctx context.Context) (map[string]Asset, error) {
	// 读出本厂全部原件，后面再按人筛选。
	rows, err := s.store.ListGovernedAssets(ctx)
	// 列表读不出来则整份失败，不回半截。
	if err != nil {
		return nil, err
	}
	// 同名只留先见到的未停用厂级。
	out := map[string]Asset{}
	// 只收未停用厂级，个人级不参与同名对照。
	for _, a := range rows {
		// 个人级和停用的不参与同名对照。
		if a.Level != AssetLevelFactory || a.Status == AssetDisabled {
			continue
		}
		// 用种类加显示名当同名对照，避免工艺和工程混在一起。
		k := importNameKey(a.Kind, a.Name)
		// 同种类同名只留先见到的那一条。
		if _, ok := out[k]; ok {
			continue
		}
		// 这个种类加名字还没有对照，就留下。
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
	// 用种类加显示名当同名对照，避免工艺和工程混在一起。
	names[importNameKey(row.Kind, row.Name)] = row
}

// 导入路径到已入库工艺的对照，含去掉前缀的别名。
type pathIndex struct {
	byPath map[string]uuid.UUID // 相对路径对应的工艺身份
	files  []legacyMapped       // 原始相对路径，用来收间隙带，不含别名
}

// 一份已入库旧工艺的原始路径和身份。
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
	// 留下原始路径，后面收缝隙带时不用别名。
	idx.files = append(idx.files, legacyMapped{Path: rel, ID: id})
	// 原始相对路径对到入库后的身份。
	idx.byPath[rel] = id
	// 再记一份去掉工艺前缀的别名，方便对旧路径。
	trimmed := strings.TrimPrefix(rel, "processes/")
	// 去掉前缀的路径同样能对上。
	idx.byPath[trimmed] = id
	// 没有子目录时，文件名也能对上旧路径。
	if !strings.Contains(trimmed, "/") {
		// 没有子目录时，文件名也能对上。
		idx.byPath[path.Base(rel)] = id
	}
}

// lookup 空路径算未选；对不上则失败。
func (idx *pathIndex) lookup(p string) (uuid.UUID, bool) {
	// 统一斜杠并去掉无意义前缀，空路径算无效。
	p = normalizeLegacyPath(p)
	// 空路径表示没选工艺，不算失败。
	if p == "" {
		return uuid.Nil, true
	}
	// 原始路径对上了已入库工艺。
	if id, ok := idx.byPath[p]; ok {
		return id, true
	}
	// 去掉工艺前缀后再试一次。
	if id, ok := idx.byPath[strings.TrimPrefix(p, "processes/")]; ok {
		return id, true
	}
	// 补上工艺前缀后再试一次。
	if id, ok := idx.byPath["processes/"+p]; ok {
		return id, true
	}
	return uuid.Nil, false
}

// gapBands 按缝隙目录收打底/盖面引用；缺文件的带仍可留下空引用。
func (idx *pathIndex) gapBands() []any {
	// 一条缝隙带记下目录、层号、间隙和两份工艺。
	type folder struct {
		name      string    // 缝隙目录名，用来归到同一条带
		layer     int       // 从目录名取出的层号
		min, max  float64   // 这条缝隙带的下限和上限
		root, cap uuid.UUID // 打底和盖面两份工艺身份
	}
	// 按缝隙目录归打底和盖面。
	folders := map[string]*folder{}
	// 只处理缝隙目录里的打底和盖面。
	for _, item := range idx.files {
		// 用原始相对路径认缝隙目录，不用别名。
		p := item.Path
		// 这条工艺入库后的身份，打底或盖面要用。
		id := item.ID
		// 拆出目录和文件，缝隙带按目录归。
		dir, file := path.Split(strings.TrimSuffix(p, "/"))
		// 去掉尾部斜杠，目录名才能对上缝隙格式。
		dir = strings.TrimSuffix(dir, "/")
		// 只看最后一层目录名是不是层号加间隙。
		base := path.Base(dir)
		// 看目录名是不是层号加间隙，不是就跳过。
		m := gapFolderRe.FindStringSubmatch(base)
		// 目录名不是层号加间隙的格式就跳过。
		if m == nil {
			continue
		}
		// 不是 T 排目录时，只留那个固定间隙样例。
		if !strings.Contains(dir, "T排立对接") && !strings.Contains(strings.ToLower(dir), "tbar") {
			// 样例目录也对不上就跳过，不收成带。
			if !strings.Contains(dir, "6T1.2") {
				continue
			}
		}
		// 从目录名取出层号，排缝隙带要用。
		layer, _ := strconv.Atoi(m[1])
		// 从目录名取出这条带的间隙下限。
		min, _ := strconv.ParseFloat(m[2], 64)
		// 从目录名取出这条带的间隙上限。
		max, _ := strconv.ParseFloat(m[3], 64)
		// 同一缝隙目录收成一条带。
		f := folders[dir]
		// 这条目录还没有带，下面按层号和间隙新建。
		if f == nil {
			// 这条目录还没有带，按层号和间隙新建。
			f = &folder{name: base, layer: layer, min: min, max: max}
			// 按目录记下这条缝隙带。
			folders[dir] = f
		}
		// 去掉后缀后看是打底还是盖面。
		name := strings.TrimSuffix(file, path.Ext(file))
		// 按文件名把工艺归到打底或盖面。
		switch {
		// 名字里有打底，就当成这条带的打底工艺。
		case strings.Contains(name, "打底"):
			// 文件名里有打底，就把这份工艺当成打底。
			f.root = id
		// 名字里有盖面，就当成这条带的盖面工艺。
		case strings.Contains(name, "盖面"):
			// 文件名里有盖面，就把这份工艺当成盖面。
			f.cap = id
		}
	}
	// 只把有打底或盖面的带拿去排序。
	var list []*folder
	// 没有打底也没有盖面的目录丢掉。
	for _, f := range folders {
		// 打底和盖面都没有的目录不写出间隙。
		if f.root == uuid.Nil && f.cap == uuid.Nil {
			continue
		}
		// 这条带至少有打底或盖面，留着写出间隙。
		list = append(list, f)
	}
	// 层号小的在前，同层再按间隙下限排。
	sort.Slice(list, func(i, j int) bool {
		// 层号不同时，层号小的排前面。
		if list[i].layer != list[j].layer {
			return list[i].layer < list[j].layer
		}
		return list[i].min < list[j].min
	})
	// 按缝隙带准备写回工程的间隙列表。
	out := make([]any, 0, len(list))
	// 每条缝隙带写成间隙和工艺身份。
	for _, f := range list {
		// 先放间隙和层号，工艺身份有则再填。
		band := map[string]any{"minGap": f.min, "maxGap": f.max, "layer": float64(f.layer), "rootProcessId": "", "capProcessId": ""}
		// 有打底才把工艺身份写进间隙带。
		if f.root != uuid.Nil {
			// 打底工艺换成身份，没有就留空。
			band["rootProcessId"] = f.root.String()
		}
		// 有盖面才把工艺身份写进间隙带。
		if f.cap != uuid.Nil {
			// 盖面工艺换成身份，没有就留空。
			band["capProcessId"] = f.cap.String()
		}
		// 这条间隙带放进工程要写回的列表。
		out = append(out, band)
	}
	return out
}

// rewriteLegacyProject 把 processPath 换成身份，删掉内嵌工艺对象，并打上模版身份。
func rewriteLegacyProject(raw []byte, filePath string, idx *pathIndex, bands []any) ([]byte, error) {
	// 先解成通用结构，再判断是不是工程数组。
	var v any
	// 这一步失败则停下，避免带着残缺结果继续。
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, domain.ErrForbidden
	}
	// 工程正文必须是数组，不是就整份拒绝。
	arr, ok := v.([]any)
	// 正文不是数组则整份工程拒绝，不把坏文件入库。
	if !ok {
		return nil, domain.ErrForbidden
	}
	// 按路径判断整份是多层、T 排还是留给逐条推断。
	fileKind := projectFileKind(filePath)
	// 这一步失败则停下，避免带着残缺结果继续。
	if err := rewriteRefs(arr, idx); err != nil {
		return nil, err
	}
	// 逐条打上模版身份，种类从路径或内容来。
	for _, el := range arr {
		// 不是对象的元素跳过，不拿来打模版。
		obj, _ := el.(map[string]any)
		// 不是对象的元素跳过，不拿来打模版。
		if obj == nil {
			continue
		}
		// 整份路径能判断种类就全用它。
		kind := fileKind
		// 整份路径判断不了种类，就按这一条自己推断。
		if kind == "" {
			// 路径判断不了，就按这一条的内容推断。
			kind = contenttpl.InferItemKind(obj)
		}
		// 按种类钉上模版身份，T 排再补间隙带。
		stampTemplate(obj, kind, bands)
	}
	// 改写完再编回正文，失败则整份工程不入。
	out, err := json.Marshal(arr)
	// 这一步失败则停下，避免带着残缺结果继续。
	if err != nil {
		return nil, err
	}
	return out, nil
}

// stampTemplate 按种类钉空库模版身份；T 排补上当时间隙带。
func stampTemplate(obj map[string]any, kind string, bands []any) {
	// 按种类钉模版，T 排还要补间隙带。
	switch kind {
	// 多层焊钉上多层模版。
	case contenttpl.ItemMulti:
		// 多层焊钉上多层模版，避免被当成单道。
		obj["templateId"] = contenttpl.SeedTplMulti
		// 标明这是多层焊。
		obj["kind"] = contenttpl.ItemMulti
	// T 排钉上 T 排模版，并准备间隙带。
	case contenttpl.ItemTBar:
		// T 排钉上 T 排模版。
		obj["templateId"] = contenttpl.SeedTplTBar
		// 标明这是 T 排。
		obj["kind"] = contenttpl.ItemTBar
		// 正文没有间隙带时，用目录里收来的补上。
		if _, ok := obj["gapBands"]; !ok {
			// 正文没有间隙带时，用目录里收来的补上。
			obj["gapBands"] = bands
		}
	// 其余当成单道，钉上单道模版。
	default:
		// 其余当成单道，钉上单道模版。
		obj["templateId"] = contenttpl.SeedTplSingle
		// 标明这一条是单道焊。
		obj["kind"] = contenttpl.ItemSingle
	}
}

// rewriteRefs 非空 processPath 必须能对上已入工艺，否则整份工程拒绝。
func rewriteRefs(v any, idx *pathIndex) error {
	// 数组继续往下，对象里才改工艺路径。
	switch x := v.(type) {
	// 数组里每一层都要改写，一处失败整份拒绝。
	case []any:
		// 数组里的每一层都要改写，一处失败整份拒绝。
		for _, el := range x {
			// 这一步失败则停下，避免带着残缺结果继续。
			if err := rewriteRefs(el, idx); err != nil {
				return err
			}
		}
	// 对象里才有工艺路径，对不上就整份拒绝。
	case map[string]any:
		// 有旧路径就必须对上已入库工艺，否则整份拒绝。
		if raw, ok := x["processPath"]; ok {
			// 路径不是字符串就当没选工艺。
			s, _ := raw.(string)
			// 把正文里的引用钉到已入库工艺。
			id, found := idx.lookup(s)
			// 路径对不上已入库工艺则整份工程拒绝。
			if !found {
				return domain.ErrAssetDependency
			}
			// 旧路径不能留在正文里，改成身份。
			delete(x, "processPath")
			// 没选工艺就写成空身份，不留旧路径。
			if s == "" || id == uuid.Nil {
				// 没选工艺就明示空身份，不留旧路径。
				x["processId"] = ""
			} else {
				// 对上的工艺写成身份，后面组包靠它。
				x["processId"] = id.String()
			}
		}
		// 内嵌工艺对象要删掉，避免把参数拆出去。
		if proc, ok := x["process"]; ok {
			// 内嵌的工艺对象删掉，避免把参数拆出去。
			if _, isObj := proc.(map[string]any); isObj {
				// 内嵌的工艺对象删掉，避免把参数拆出去。
				delete(x, "process")
			}
		}
		// 数组里的每一层都要改写，一处失败整份拒绝。
		for k, child := range x {
			// 已经换成身份的字段不再往下改。
			if k == "processId" {
				continue
			}
			// 这一步失败则停下，避免带着残缺结果继续。
			if err := rewriteRefs(child, idx); err != nil {
				return err
			}
		}
	}
	return nil
}

// projectFileKind 按相对路径判断整份种类；空则逐条推断。
func projectFileKind(p string) string {
	// 统一斜杠并去掉无意义前缀，空路径算无效。
	n := strings.ToLower(normalizeLegacyPath(p))
	// 路径像多层焊，整份都按多层处理。
	if strings.HasSuffix(n, "multilayer_data.json") || strings.Contains(n, "/multi_layer/") || strings.Contains(n, "/multilayer/") {
		return contenttpl.ItemMulti
	}
	// 路径在 T 排目录里，整份按 T 排处理。
	if strings.Contains(n, "/tbar/") || strings.HasPrefix(n, "tbar/") {
		return contenttpl.ItemTBar
	}
	return ""
}

// normalizeLegacyPath 统一斜杠并去掉无意义的前缀。
func normalizeLegacyPath(p string) string {
	// 反斜杠改成斜杠，后面才能按相对路径认。
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	// 去掉无意义的当前目录前缀。
	p = strings.TrimPrefix(p, "./")
	// 折叠多余的点号和斜杠，避免同一文件对成两条。
	p = path.Clean(p)
	// 清成当前目录则当无效路径。
	if p == "." {
		return ""
	}
	// 去掉开头的斜杠，路径相对工艺根。
	return strings.TrimPrefix(p, "/")
}

// processDisplayName 优先用工艺 JSON 的 name，否则用文件名。
func processDisplayName(body []byte, rel string) string {
	// 准备从工艺正文里取显示名。
	var obj map[string]any
	// 正文能解开才看里面的名字，解不开就用文件名。
	if json.Unmarshal(body, &obj) == nil {
		// 正文里有名字就用它，不再用文件名。
		if n, _ := obj["name"].(string); strings.TrimSpace(n) != "" {
			// 用正文里的名字，两端空白去掉。
			return strings.TrimSpace(n)
		}
	}
	// 正文没有名字就用文件名。
	base := path.Base(rel)
	// 去掉后缀当显示名。
	return strings.TrimSuffix(base, path.Ext(base))
}

// projectDisplayName 用工程所在文件夹名。
func projectDisplayName(rel string) string {
	// 工程显示名用所在文件夹。
	dir := path.Dir(normalizeLegacyPath(rel))
	// 文件夹名能用就拿它当工程显示名。
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
	// 用去掉后缀的相对路径当候选新名。
	label := strings.TrimSpace(legacyPathLabel(rel))
	// 路径名和原名不同，才考虑拿路径另起。
	if label != "" && label != name {
		// 这个路径名还没被占用，就用它另起。
		if _, ok := names[importNameKey(kind, label)]; !ok {
			return label
		}
		// 路径名也被占用了，后面再加序号。
		name = label
	}
	// 仍撞名就加序号，直到找出空名。
	for i := 2; i < 1000; i++ {
		// 仍撞名就加序号再试。
		n := name + " (" + strconv.Itoa(i) + ")"
		if _, ok := names[importNameKey(kind, n)]; !ok {
			return n
		}
	}
	return name
}

// legacyPathLabel 用去掉后缀的相对路径当新名；多层工程另标一层。
func legacyPathLabel(rel string) string {
	// 统一斜杠并去掉无意义前缀，空路径算无效。
	rel = normalizeLegacyPath(rel)
	// 没有路径就不起新名。
	if rel == "" {
		return ""
	}
	// 去掉后缀，准备用相对路径当新名。
	stem := strings.TrimSuffix(rel, path.Ext(rel))
	// 看文件名是不是整份工程的固定名字。
	base := path.Base(stem)
	// 不是整份工程的固定文件名时，用相对路径当新名。
	if base != "project_data" && base != "multilayer_data" {
		return stem
	}
	// 固定文件名时改用所在文件夹当名字。
	dir := path.Dir(stem)
	// 没有文件夹时就用文件名本身。
	if dir == "." || dir == "/" {
		// 没有文件夹时就用文件名本身。
		dir = base
	}
	// 多层工程的名字带上层标记，避免和单道撞名。
	if base == "multilayer_data" {
		return dir + "-多层"
	}
	return dir
}
