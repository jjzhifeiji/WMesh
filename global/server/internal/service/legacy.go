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
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "import_legacy", "wan", audit.Deny)
		return LegacyImportResult{}, err
	}
	out := LegacyImportResult{Processes: []Asset{}, Projects: []Asset{}, Rejected: []LegacyReject{}, Skipped: []LegacyReject{}}
	names, err := s.platformImportNames(ctx)
	if err != nil {
		return LegacyImportResult{}, err
	}
	idx := newPathIndex()
	seen := map[string]struct{}{}
	for _, f := range in.Processes {
		rel := normalizeLegacyPath(f.Path)
		if rel == "" {
			out.Rejected = append(out.Rejected, LegacyReject{Path: f.Path, Reason: "invalid path"})
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		if !json.Valid(f.Content) {
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: "invalid process json"})
			continue
		}
		name := strings.TrimSpace(f.Name)
		if name == "" {
			name = processDisplayName(f.Content, rel)
		}
		overwrite, rename := importFileFlags(in, f)
		if cur, ok := names[importNameKey(KindProcess, name)]; ok {
			if !overwrite {
				if !rename {
					idx.add(rel, cur.ID)
					out.Skipped = append(out.Skipped, LegacyReject{Path: rel, Reason: "name exists"})
					continue
				}
				name = uniqueImportName(names, KindProcess, name, rel)
			} else {
				row, err := s.overwriteImported(ctx, token, cur, f.Content)
				if err != nil {
					out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
					continue
				}
				rememberImportName(names, row)
				idx.add(rel, row.ID)
				out.Processes = append(out.Processes, row)
				continue
			}
		}
		row, err := s.CreatePlatformProcess(ctx, token, name, f.Content)
		if err != nil {
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		row, err = s.PublishPlatformAsset(ctx, token, row.ID, row.Revision)
		if err != nil {
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		rememberImportName(names, row)
		idx.add(rel, row.ID)
		out.Processes = append(out.Processes, row)
	}
	bands := idx.gapBands()
	for _, f := range in.Projects {
		rel := normalizeLegacyPath(f.Path)
		name := strings.TrimSpace(f.Name)
		if name == "" {
			name = projectDisplayName(rel)
		}
		overwrite, rename := importFileFlags(in, f)
		if _, ok := names[importNameKey(KindProject, name)]; ok && !overwrite {
			if !rename {
				out.Skipped = append(out.Skipped, LegacyReject{Path: rel, Reason: "name exists"})
				continue
			}
			name = uniqueImportName(names, KindProject, name, rel)
		}
		body, err := rewriteLegacyProject(f.Content, rel, idx, bands)
		if err != nil {
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		if cur, ok := names[importNameKey(KindProject, name)]; ok {
			row, err := s.overwriteImported(ctx, token, cur, body)
			if err != nil {
				out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
				continue
			}
			rememberImportName(names, row)
			out.Projects = append(out.Projects, row)
			continue
		}
		row, err := s.insertImportedProject(ctx, token, name, body)
		if err != nil {
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		row, err = s.PublishPlatformAsset(ctx, token, row.ID, row.Revision)
		if err != nil {
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: err.Error()})
			continue
		}
		rememberImportName(names, row)
		out.Projects = append(out.Projects, row)
	}
	target := "processes=" + strconv.Itoa(len(out.Processes)) + " projects=" + strconv.Itoa(len(out.Projects)) + " rejected=" + strconv.Itoa(len(out.Rejected)) + " skipped=" + strconv.Itoa(len(out.Skipped))
	if err := s.audit(ctx, &admin.ID, nil, nil, "import_legacy", target, audit.Allow); err != nil {
		return LegacyImportResult{}, err
	}
	return out, nil
}

// insertImportedProject 按改写后正文落平台级工程，不套模版，免把 T 排元素丢掉。
func (s *Assets) insertImportedProject(ctx context.Context, token, name string, content []byte) (Asset, error) {
	admin, err := s.RequireAdmin(ctx, token)
	if err != nil {
		_ = s.audit(ctx, nil, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	deps, err := s.importedProjectDeps(ctx, admin.ID, name, content)
	if err != nil {
		return Asset{}, err
	}
	row, err := s.store.InsertAsset(ctx, Asset{
		Kind: KindProject, Name: name, Status: AssetDraft,
		Content: content, Digest: digest.Sum(content), CreatorID: admin.ID, Deps: deps,
	})
	if err != nil {
		_ = s.audit(ctx, &admin.ID, nil, nil, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	if err := s.audit(ctx, &admin.ID, nil, nil, "create_asset", assetTarget(row.ID, row.Revision), audit.Allow); err != nil {
		return Asset{}, err
	}
	return stripContent(row), nil
}

// overwriteImported 改正文（工程同时改依赖），身份不变；草稿顺带发布。
func (s *Assets) overwriteImported(ctx context.Context, token string, cur Asset, content []byte) (Asset, error) {
	deps := []AssetDep{}
	if cur.Kind == KindProject {
		d, err := s.importedProjectDeps(ctx, uuid.Nil, cur.Name, content)
		if err != nil {
			return Asset{}, err
		}
		deps = d
	}
	row, err := s.mutatePlatform(ctx, token, cur.ID, cur.Revision, "update_asset", func(live Asset) (store.AssetWrite, error) {
		if live.Status == AssetDisabled {
			return store.AssetWrite{}, domain.ErrAssetNotAvailable
		}
		if live.Kind == KindProject {
			if err := s.assertProjectProcessIDs(ctx, content, deps); err != nil {
				return store.AssetWrite{}, err
			}
		}
		return store.AssetWrite{Name: live.Name, Content: content, Digest: digest.Sum(content), Copyable: live.Copyable, Status: live.Status, Deps: deps}, nil
	})
	if err != nil {
		return Asset{}, err
	}
	if row.Status == AssetDraft {
		return s.PublishPlatformAsset(ctx, token, row.ID, row.Revision)
	}
	return row, nil
}

// importedProjectDeps 按改写后正文钉当前可用平台工艺，不套模版。
func (s *Assets) importedProjectDeps(ctx context.Context, adminID uuid.UUID, name string, content []byte) ([]AssetDep, error) {
	ids, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), content)
	if err != nil {
		if adminID != uuid.Nil {
			_ = s.audit(ctx, &adminID, nil, nil, "create_asset", name, audit.Deny)
		}
		return nil, domain.ErrAssetDependency
	}
	deps, err := mergeProjectDeps(nil, ids, func(id uuid.UUID) (AssetDep, error) {
		p, err := s.loadChecked(ctx, id)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return AssetDep{}, domain.ErrAssetDependency
			}
			return AssetDep{}, err
		}
		if p.Kind != KindProcess {
			return AssetDep{}, domain.ErrAssetDependency
		}
		if p.Status != AssetAvailable {
			return AssetDep{}, domain.ErrAssetNotAvailable
		}
		return AssetDep{ID: p.ID, Revision: p.Revision, Digest: p.Digest}, nil
	})
	if err != nil {
		if adminID != uuid.Nil {
			_ = s.audit(ctx, &adminID, nil, nil, "create_asset", name, audit.Deny)
		}
		return nil, err
	}
	if err := s.assertPlatformProcessDeps(ctx, deps); err != nil {
		if adminID != uuid.Nil {
			_ = s.audit(ctx, &adminID, nil, nil, "create_asset", name, audit.Deny)
		}
		return nil, err
	}
	allowed := map[string]struct{}{}
	for _, d := range deps {
		allowed[d.ID.String()] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := allowed[id]; !ok {
			if adminID != uuid.Nil {
				_ = s.audit(ctx, &adminID, nil, nil, "create_asset", name, audit.Deny)
			}
			return nil, domain.ErrAssetDependency
		}
	}
	return deps, nil
}

// platformImportNames 未停用平台级按种类+显示名取最新一条。
func (s *Assets) platformImportNames(ctx context.Context) (map[string]Asset, error) {
	rows, err := s.store.ListAssets(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]Asset{}
	for _, a := range rows {
		if a.Status == AssetDisabled {
			continue
		}
		k := importNameKey(a.Kind, a.Name)
		if _, ok := out[k]; ok {
			continue
		}
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
	names[importNameKey(row.Kind, row.Name)] = row
}

type pathIndex struct {
	byPath map[string]uuid.UUID
	files  []legacyMapped // 原始相对路径，用来收间隙带，不含别名
}

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
	idx.files = append(idx.files, legacyMapped{Path: rel, ID: id})
	idx.byPath[rel] = id
	trimmed := strings.TrimPrefix(rel, "processes/")
	idx.byPath[trimmed] = id
	if !strings.Contains(trimmed, "/") {
		idx.byPath[path.Base(rel)] = id
	}
}

// lookup 空路径算未选；对不上则失败。
func (idx *pathIndex) lookup(p string) (uuid.UUID, bool) {
	p = normalizeLegacyPath(p)
	if p == "" {
		return uuid.Nil, true
	}
	if id, ok := idx.byPath[p]; ok {
		return id, true
	}
	if id, ok := idx.byPath[strings.TrimPrefix(p, "processes/")]; ok {
		return id, true
	}
	if id, ok := idx.byPath["processes/"+p]; ok {
		return id, true
	}
	return uuid.Nil, false
}

// gapBands 按缝隙目录收打底/盖面引用；缺文件的带仍可留下空引用。
func (idx *pathIndex) gapBands() []any {
	type folder struct {
		name      string
		layer     int
		min, max  float64
		root, cap uuid.UUID
	}
	folders := map[string]*folder{}
	for _, item := range idx.files {
		p := item.Path
		id := item.ID
		dir, file := path.Split(strings.TrimSuffix(p, "/"))
		dir = strings.TrimSuffix(dir, "/")
		base := path.Base(dir)
		m := gapFolderRe.FindStringSubmatch(base)
		if m == nil {
			continue
		}
		if !strings.Contains(dir, "T排立对接") && !strings.Contains(strings.ToLower(dir), "tbar") {
			if !strings.Contains(dir, "6T1.2") {
				continue
			}
		}
		layer, _ := strconv.Atoi(m[1])
		min, _ := strconv.ParseFloat(m[2], 64)
		max, _ := strconv.ParseFloat(m[3], 64)
		f := folders[dir]
		if f == nil {
			f = &folder{name: base, layer: layer, min: min, max: max}
			folders[dir] = f
		}
		name := strings.TrimSuffix(file, path.Ext(file))
		switch {
		case strings.Contains(name, "打底"):
			f.root = id
		case strings.Contains(name, "盖面"):
			f.cap = id
		}
	}
	var list []*folder
	for _, f := range folders {
		if f.root == uuid.Nil && f.cap == uuid.Nil {
			continue
		}
		list = append(list, f)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].layer != list[j].layer {
			return list[i].layer < list[j].layer
		}
		return list[i].min < list[j].min
	})
	out := make([]any, 0, len(list))
	for _, f := range list {
		band := map[string]any{"minGap": f.min, "maxGap": f.max, "layer": float64(f.layer), "rootProcessId": "", "capProcessId": ""}
		if f.root != uuid.Nil {
			band["rootProcessId"] = f.root.String()
		}
		if f.cap != uuid.Nil {
			band["capProcessId"] = f.cap.String()
		}
		out = append(out, band)
	}
	return out
}

// rewriteLegacyProject 把 processPath 换成身份，删掉内嵌工艺对象，并打上模版身份。
func rewriteLegacyProject(raw []byte, filePath string, idx *pathIndex, bands []any) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, domain.ErrForbidden
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, domain.ErrForbidden
	}
	fileKind := projectFileKind(filePath)
	if err := rewriteRefs(arr, idx); err != nil {
		return nil, err
	}
	for _, el := range arr {
		obj, _ := el.(map[string]any)
		if obj == nil {
			continue
		}
		kind := fileKind
		if kind == "" {
			kind = contenttpl.InferItemKind(obj)
		}
		stampTemplate(obj, kind, bands)
	}
	out, err := json.Marshal(arr)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// stampTemplate 按种类钉空库模版身份；T 排补上当时间隙带。
func stampTemplate(obj map[string]any, kind string, bands []any) {
	switch kind {
	case contenttpl.ItemMulti:
		obj["templateId"] = contenttpl.SeedTplMulti
		obj["kind"] = contenttpl.ItemMulti
	case contenttpl.ItemTBar:
		obj["templateId"] = contenttpl.SeedTplTBar
		obj["kind"] = contenttpl.ItemTBar
		if _, ok := obj["gapBands"]; !ok {
			obj["gapBands"] = bands
		}
	default:
		obj["templateId"] = contenttpl.SeedTplSingle
		obj["kind"] = contenttpl.ItemSingle
	}
}

// rewriteRefs 非空 processPath 必须能对上已入工艺，否则整份工程拒绝。
func rewriteRefs(v any, idx *pathIndex) error {
	switch x := v.(type) {
	case []any:
		for _, el := range x {
			if err := rewriteRefs(el, idx); err != nil {
				return err
			}
		}
	case map[string]any:
		if raw, ok := x["processPath"]; ok {
			s, _ := raw.(string)
			id, found := idx.lookup(s)
			if !found {
				return domain.ErrAssetDependency
			}
			delete(x, "processPath")
			if s == "" || id == uuid.Nil {
				x["processId"] = ""
			} else {
				x["processId"] = id.String()
			}
		}
		if proc, ok := x["process"]; ok {
			if _, isObj := proc.(map[string]any); isObj {
				delete(x, "process")
			}
		}
		for k, child := range x {
			if k == "processId" {
				continue
			}
			if err := rewriteRefs(child, idx); err != nil {
				return err
			}
		}
	}
	return nil
}

// projectFileKind 按相对路径判断整份种类；空则逐条推断。
func projectFileKind(p string) string {
	n := strings.ToLower(normalizeLegacyPath(p))
	if strings.HasSuffix(n, "multilayer_data.json") || strings.Contains(n, "/multi_layer/") || strings.Contains(n, "/multilayer/") {
		return contenttpl.ItemMulti
	}
	if strings.Contains(n, "/tbar/") || strings.HasPrefix(n, "tbar/") {
		return contenttpl.ItemTBar
	}
	return ""
}

// normalizeLegacyPath 统一斜杠并去掉无意义的前缀。
func normalizeLegacyPath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimPrefix(p, "./")
	p = path.Clean(p)
	if p == "." {
		return ""
	}
	return strings.TrimPrefix(p, "/")
}

// processDisplayName 优先用工艺 JSON 的 name，否则用文件名。
func processDisplayName(body []byte, rel string) string {
	var obj map[string]any
	if json.Unmarshal(body, &obj) == nil {
		if n, _ := obj["name"].(string); strings.TrimSpace(n) != "" {
			return strings.TrimSpace(n)
		}
	}
	base := path.Base(rel)
	return strings.TrimSuffix(base, path.Ext(base))
}

// projectDisplayName 用工程所在文件夹名。
func projectDisplayName(rel string) string {
	dir := path.Dir(normalizeLegacyPath(rel))
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
	label := strings.TrimSpace(legacyPathLabel(rel))
	if label != "" && label != name {
		if _, ok := names[importNameKey(kind, label)]; !ok {
			return label
		}
		name = label
	}
	for i := 2; i < 1000; i++ {
		n := name + " (" + strconv.Itoa(i) + ")"
		if _, ok := names[importNameKey(kind, n)]; !ok {
			return n
		}
	}
	return name
}

// legacyPathLabel 用去掉后缀的相对路径当新名；多层工程另标一层。
func legacyPathLabel(rel string) string {
	rel = normalizeLegacyPath(rel)
	if rel == "" {
		return ""
	}
	stem := strings.TrimSuffix(rel, path.Ext(rel))
	base := path.Base(stem)
	if base != "project_data" && base != "multilayer_data" {
		return stem
	}
	dir := path.Dir(stem)
	if dir == "." || dir == "/" {
		dir = base
	}
	if base == "multilayer_data" {
		return dir + "-多层"
	}
	return dir
}
