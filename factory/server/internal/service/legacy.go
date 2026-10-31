package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path"
	"regexp"
	"slices"
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

var importSerialRe = regexp.MustCompile(`^(.+) \(\d+\)$`) // 另起名时加在末尾的「 (序号)」

// errTBarGapBandsMissing T 排元素没带间隙带、本批又没有缝隙目录工艺可补，只作这份工程的拒绝原因。
var errTBarGapBandsMissing = errors.New("tbar gap bands missing: import gap folder processes together with the project")

// rejectReason 单条拒绝原因：业务错误只给哨兵原话，与 writeErr 同规则；
// 库连接中断、正文含 0x00 之类内部故障只进日志，对外一律 internal error。
func rejectReason(err error) string {
	// 认得出的业务错误给哨兵原话，包进去的细节不带出去。
	if known := domain.Known(err); known != nil {
		// 交回哨兵原话，前端和示教器照常按文字显示。
		return known.Error()
	}
	// T 排缺间隙带是导入自己的拒绝原因，提示要一起导缝隙目录工艺。
	if errors.Is(err, errTBarGapBandsMissing) {
		// 交回这句固定提示，不含库或正文细节。
		return errTBarGapBandsMissing.Error()
	}
	// 其余是内部故障，原文可能带 SQLSTATE、库用户名或内网地址，只记日志。
	slog.Error("item rejected", "err", err)
	return "internal error"
}

// LegacyFile 是一次导入里的一份旧 JSON，路径相对工艺/工程根。
type LegacyFile struct {
	Path      string // 相对路径，如 Standard/1T1.2/1K.json 或 tbar/x/project_data.json
	Name      string // 给人看的名字；空则从路径或正文取
	Content   []byte // UTF-8 JSON
	Overwrite bool   // 本份同名改正文；可盖过整批
	Rename    bool   // 本份同名按路径另起；可盖过整批
	WeldKind  string // 重试工艺时带回的作业类型，只处理这一份；空则按本批预扫
	Suffix    string // 和作业类型一起带回的名字后缀，首种那份为空
}

// LegacyImport 厂端一次性导入：先工艺后工程。
type LegacyImport struct {
	Level     string       // 导进哪一级：personal 进本人个人文件夹（默认），factory 进厂级要管理员
	Processes []LegacyFile // 工艺文件，同一路径只入一次
	Projects  []LegacyFile // 工程文件；缺路径则该份拒绝
	Overwrite bool         // 同名改正文保留身份；否就跳过同名
	Rename    bool         // 同名按路径另起新名新建；覆盖优先
}

// LegacyReject 是未入库的一份工程或坏工艺，已入工艺仍保留。
type LegacyReject struct {
	Path     string `json:"path"`               // 相对路径
	Reason   string `json:"reason"`             // 英文原因，无正文
	WeldKind string `json:"weldKind,omitempty"` // 工艺按哪种作业类型那份；工程或不限类型时为空
	Suffix   string `json:"suffix,omitempty"`   // 那份工艺名字后接的作业类型后缀，首种为空
}

// LegacyImportResult 是本批导入结果；部分工程拒绝时其它份仍可成功。
type LegacyImportResult struct {
	Processes []Asset        `json:"processes"` // 新建或覆盖后的本厂工艺
	Projects  []Asset        `json:"projects"`  // 新建或覆盖后的本厂工程
	Rejected  []LegacyReject `json:"rejected"`  // 未入的工程或坏文件
	Skipped   []LegacyReject `json:"skipped"`   // 内容相同或同名跳过，沿用已有身份
}

// ImportLegacy 把旧文件收成本厂资产：默认进本人个人文件夹，选厂级要超管或管理员。
func (s *Assets) ImportLegacy(ctx context.Context, token string, in LegacyImport) (LegacyImportResult, error) {
	// 核对登录仍有效，后面的操作都靠这次会话。
	acc, err := s.RequireActive(ctx, token)
	// 登录已失效则拒绝，避免未登录的人继续。
	if err != nil {
		return LegacyImportResult{}, err
	}
	// 没选级别就进个人文件夹
	level := in.Level
	// 空着按个人级
	if level == "" {
		// 默认导进本人个人文件夹
		level = AssetLevelPersonal
	}
	// 只能导进厂级或个人级
	if level != AssetLevelFactory && level != AssetLevelPersonal {
		// 不认识的级别记拒绝
		_ = s.audit(ctx, &acc.ID, nil, "import_legacy", level, audit.Deny)
		return LegacyImportResult{}, domain.ErrForbidden
	}
	// 按级别核制作权，厂级要管理员
	if err := s.canAuthorLevel(ctx, acc, level); err != nil {
		// 没有厂级制作权则记拒绝
		_ = s.audit(ctx, &acc.ID, nil, "import_legacy", level, audit.Deny)
		return LegacyImportResult{}, err
	}
	// 先放空结果，工艺和工程分开，失败的另记。
	out := LegacyImportResult{
		Processes: []Asset{},
		Projects:  []Asset{},
		Rejected:  []LegacyReject{},
		Skipped:   []LegacyReject{},
	}
	// 取出同一级未停用的同名对照和工艺；个人级只看本人的。
	book, err := s.importBook(ctx, acc, level)
	// 对照表读失败则整批导入停下。
	if err != nil {
		return LegacyImportResult{}, err
	}
	// 准备路径对照，工程靠它把旧路径换成身份。
	idx := newPathIndex()
	// 导入挂到对应树根
	wc := WorkContext{}
	// 同一相对路径只收一次，重复的丢掉。
	seen := map[string]struct{}{}
	// 先预扫本批工程，记下每份工艺被哪些作业类型引用。
	uses := scanLegacyUses(in.Processes, in.Projects)
	// 按相对路径排好再收，同名时谁留原名每次都一样。
	for _, f := range sortLegacyFiles(in.Processes) {
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
		// 先备好这份工艺的公共部分，各作业类型那份共用。
		p := legacyProc{rel: rel, base: name, content: f.Content, sums: s.importDigests(ctx, f.Content), overwrite: overwrite, rename: rename}
		// 重试带回了作业类型，只处理那一份，不看本批预扫。
		if f.WeldKind != "" || f.Suffix != "" {
			// 作业类型不合法或后缀对不上就拒绝这一份。
			kind, ok := legacyRetryKind(f)
			// 带回的类型或后缀不对，记入未入清单。
			if !ok {
				// 这一份没入库，记入未入清单，其它份继续。
				out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: "invalid weld kind", WeldKind: f.WeldKind, Suffix: f.Suffix})
				continue
			}
			// 按带回的类型和后缀定位首导时那一份。
			p.kind, p.suffix = kind, f.Suffix
			// 只按这种作业类型重试这一份。
			s.importLegacyProcess(ctx, acc, token, wc, level, book, idx, &out, p)
			continue
		}
		// 本批没有工程引用就只入一份，不限类型沿用。
		if len(uses[rel]) == 0 {
			// 不限类型找能沿用的，找不到按单层新建。
			s.importLegacyProcess(ctx, acc, token, wc, level, book, idx, &out, p)
			continue
		}
		// 被几种作业类型引用就按类型各入一份。
		for i, kind := range uses[rel] {
			// 这一份按这种作业类型比对和新建。
			p.kind = kind
			// 第一种用原名，不加后缀。
			p.suffix = ""
			// 不是第一种就加后缀，免得和前一份撞名。
			if i > 0 {
				// 原名后面要接的作业类型后缀。
				p.suffix = weldKindSuffix(kind)
			}
			// 按这种作业类型入一份并记进路径对照。
			s.importLegacyProcess(ctx, acc, token, wc, level, book, idx, &out, p)
		}
	}
	// 按缝隙目录收 T 排那份打底和盖面，缺文件的带也可以留。
	bands := idx.gapBands()
	// 工艺入库后再收工程，同样按相对路径排好。
	for _, f := range sortLegacyFiles(in.Projects) {
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
		// 名字已被本批别的路径占用，按路径另起，不跳过也不覆盖。
		if book.takenByBatch(KindProject, name) {
			// 先改用路径名，路径名也被本批占用才加序号。
			name = book.batchFreeName(KindProject, name, rel, "")
		}
		// 已有同名工程且不覆盖，再看是否另起名。
		if cur, ok := book.names[importNameKey(KindProject, name)]; ok && !overwrite {
			// 不另起名就跳过同名，沿用已有那一条。
			if !rename {
				// 沿用的身份算本批占用，后面同名另起。
				book.claim(cur)
				// 同名且不覆盖就跳过，沿用已有身份。
				out.Skipped = append(out.Skipped, LegacyReject{Path: rel, Reason: "name exists"})
				continue
			}
			// 同名按路径另起，仍撞就加序号。
			name = uniqueImportName(book.names, KindProject, name, rel, "")
		}
		// 把旧路径换成身份并打上模版，失败则整份不入。
		body, err := rewriteLegacyProject(f.Content, rel, idx, bands)
		// 这一份失败就记入未入清单，其它份继续。
		if err != nil {
			// 这一份没入库，记入未入清单，其它份继续。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: rejectReason(err)})
			continue
		}
		// 覆盖时改正文，工程身份保持不变。
		if cur, ok := book.names[importNameKey(KindProject, name)]; ok {
			// 同名且允许覆盖时改正文，身份保持不变。
			row, err := s.overwriteImported(ctx, acc, token, cur, body)
			// 这一份失败就记入未入清单，其它份继续。
			if err != nil {
				// 这一份没入库，记入未入清单，其它份继续。
				out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: rejectReason(err)})
				continue
			}
			// 覆盖过的身份算本批占用，后面同名另起。
			book.claim(row)
			// 这份工程已经入库，放进本批成功结果。
			out.Projects = append(out.Projects, row)
			continue
		}
		// 按改写后的正文落厂级工程，不套会丢元素的模版。
		row, err := s.insertImportedProject(ctx, acc, level, name, body)
		// 这一份失败就记入未入清单，其它份继续。
		if err != nil {
			// 这一份没入库，记入未入清单，其它份继续。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: rejectReason(err)})
			continue
		}
		// 导入的草稿立刻发布成可用。
		row, err = s.PublishAsset(ctx, token, row.ID, row.Revision)
		// 这一份失败就记入未入清单，其它份继续。
		if err != nil {
			// 这一份没入库，记入未入清单，其它份继续。
			out.Rejected = append(out.Rejected, LegacyReject{Path: rel, Reason: rejectReason(err)})
			continue
		}
		// 本批新建的身份立刻参与后面的同名判断。
		book.claim(row)
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

// legacyProc 一份旧工艺按一种作业类型入库时要用的内容。
type legacyProc struct {
	rel       string   // 规范化后的相对路径
	base      string   // 原名，按摘要找沿用时用它排候选
	suffix    string   // 非首种作业类型那份加的后缀，首种为空
	kind      string   // 作业类型；空表示本批没有工程引用，不限类型
	content   []byte   // 旧工艺正文
	sums      [][]byte // 原文和套模版后的两份摘要
	overwrite bool     // 同名时改正文，身份不变
	rename    bool     // 同名时按路径另起名新建
}

// reject 这一份工艺的未入或跳过条目，带上作业类型和后缀，重试时原样带回。
func (p legacyProc) reject(reason string) LegacyReject {
	// 同一路径拆成几份时靠类型和后缀分开。
	return LegacyReject{Path: p.rel, Reason: reason, WeldKind: p.kind, Suffix: p.suffix}
}

// legacyRetryKind 收下重试带回的作业类型；类型须合法，后缀只能为空或该类型的后缀。
func legacyRetryKind(f LegacyFile) (string, bool) {
	// 没带作业类型只带后缀，对不上任何一份。
	if strings.TrimSpace(f.WeldKind) == "" {
		return "", false
	}
	// 收成单层、多层或 T 排。
	kind, err := store.NormalizeWeldKind(f.WeldKind)
	// 别的写法一律不认。
	if err != nil {
		return "", false
	}
	// 后缀只能是空或这种类型自己的后缀。
	if f.Suffix != "" && f.Suffix != weldKindSuffix(kind) {
		return "", false
	}
	return kind, true
}

// indexKind 记进路径对照的作业类型：本批引用的类型，没人引用就用条目自己的。
func (p legacyProc) indexKind(row Asset) string {
	// 本批有工程引用时，按引用的类型记。
	if p.kind != "" {
		return p.kind
	}
	// 没人引用就按条目自己的作业类型记。
	return normWeldKind(row.WeldKind)
}

// importLegacyProcess 按一种作业类型收一份旧工艺，结果记进路径对照和本批结果。
func (s *Assets) importLegacyProcess(ctx context.Context, acc Account, token string, wc WorkContext, level string, book *importBook, idx *pathIndex, out *LegacyImportResult, p legacyProc) {
	// 原名接上后缀就是这一份的名字，撞名时再改。
	name := p.base + p.suffix
	// 新建时用的作业类型，本批没人引用就按单层。
	create := p.kind
	// 没有工程引用的工艺新建时按单层。
	if create == "" {
		// 旧工艺不带作业类型，没人引用就当单层。
		create = store.WeldKindSingle
	}
	// 候选名里有摘要相同的工艺就沿用，重导不出副本。
	if hit, ok := book.sameContent(p.base, p.rel, p.kind, p.sums); ok {
		// 沿用的身份算本批占用，别的路径不再认它。
		book.claim(hit)
		// 按路径和作业类型记下，后面工程才能对上。
		idx.add(p.rel, p.indexKind(hit), hit.ID)
		// 内容相同就跳过，沿用已有身份。
		out.Skipped = append(out.Skipped, p.reject("same content"))
		return
	}
	// 名字已被本批别的路径占用，按路径另起，不跳过也不覆盖。
	if book.takenByBatch(KindProcess, name) {
		// 先改用路径名，路径名也被本批占用才加序号。
		name = book.batchFreeName(KindProcess, name, p.rel, p.suffix)
	}
	// 库里已有同名未停用工艺，再看作业类型和覆盖开关。
	if cur, ok := book.names[importNameKey(KindProcess, name)]; ok {
		// 按作业类型、覆盖、另起名、跳过依次处理。
		switch {
		// 作业类型不同则不沿用不覆盖，另起名新建；没人引用的不限类型。
		case p.kind != "" && !sameWeldKind(cur.WeldKind, p.kind):
			// 同名按路径另起，仍撞就加序号。
			name = uniqueImportName(book.names, KindProcess, name, p.rel, p.suffix)
		// 允许覆盖时改正文，身份保持不变。
		case p.overwrite:
			// 同名且允许覆盖时改正文，身份保持不变。
			row, err := s.overwriteImported(ctx, acc, token, cur, p.content)
			// 这一份失败就记入未入清单，其它份继续。
			if err != nil {
				// 这一份没入库，记入未入清单，其它份继续。
				out.Rejected = append(out.Rejected, p.reject(rejectReason(err)))
				return
			}
			// 覆盖过的身份算本批占用，后面同名另起。
			book.claim(row)
			// 按路径和作业类型记下，后面工程才能对上。
			idx.add(p.rel, p.indexKind(cur), row.ID)
			// 这份工艺已经入库，放进本批成功结果。
			out.Processes = append(out.Processes, row)
			return
		// 允许另起名时按路径另起新名新建。
		case p.rename:
			// 同名按路径另起，仍撞就加序号。
			name = uniqueImportName(book.names, KindProcess, name, p.rel, p.suffix)
		// 既不覆盖也不另起名，就沿用库里那一条。
		default:
			// 沿用的身份算本批占用，后面同名另起。
			book.claim(cur)
			// 按路径和作业类型记下，后面工程才能对上。
			idx.add(p.rel, p.indexKind(cur), cur.ID)
			// 同名且内容不同，按选择跳过并沿用已有身份。
			out.Skipped = append(out.Skipped, p.reject("name exists, content differs"))
			return
		}
	}
	// 把这份旧工艺按作业类型收成所选级别的草稿。
	row, err := s.CreateProcess(ctx, token, wc, level, name, p.content, true, create)
	// 这一份失败就记入未入清单，其它份继续。
	if err != nil {
		// 这一份没入库，记入未入清单，其它份继续。
		out.Rejected = append(out.Rejected, p.reject(rejectReason(err)))
		return
	}
	// 导入的草稿立刻发布成可用。
	row, err = s.PublishAsset(ctx, token, row.ID, row.Revision)
	// 这一份失败就记入未入清单，其它份继续。
	if err != nil {
		// 这一份没入库，记入未入清单，其它份继续。
		out.Rejected = append(out.Rejected, p.reject(rejectReason(err)))
		return
	}
	// 本批新建的身份立刻参与后面的同名判断。
	book.claim(row)
	// 按路径和作业类型记下，后面工程才能对上。
	idx.add(p.rel, create, row.ID)
	// 这份工艺已经入库，放进本批成功结果。
	out.Processes = append(out.Processes, row)
}

// insertImportedProject 按改写后正文落工程，不套已收模版，免把 T 排元素丢掉。
func (s *Assets) insertImportedProject(ctx context.Context, acc Account, level, name string, content []byte) (Asset, error) {
	// 按级别核制作权，没有则拒绝。
	if err := s.canAuthorLevel(ctx, acc, level); err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 作业类型按改写后正文里的模版推断。
	weld := contenttpl.InferWeldKind(content)
	// 按改写后的正文钉当前可用工艺，并核作业类型。
	deps, err := s.importedProjectDeps(ctx, acc, level, weld, content)
	// 失败则记拒绝并停下，不继续往下改。
	if err != nil {
		// 创建被拒时补记审计，写失败仍维持拒绝。
		_ = s.auditAt(ctx, &acc.ID, "create_asset", name, audit.Deny)
		return Asset{}, err
	}
	// 校验都过了才按推断的作业类型落成一条资产。
	return s.insertGoverned(ctx, acc, KindProject, level, name, content, deps, true, weld)
}

// overwriteImported 改正文（工程同时改依赖和作业类型），身份不变；草稿顺带发布。
func (s *Assets) overwriteImported(ctx context.Context, acc Account, token string, cur Asset, content []byte) (Asset, error) {
	// 工艺覆盖不改依赖；工程在下面重钉。
	deps := []AssetDep{}
	// 工艺覆盖不改作业类型，空着表示保持原样。
	weld := ""
	// 工程覆盖时同时改依赖，工艺只改正文。
	if cur.Kind == KindProject {
		// 工程作业类型按新正文推断，一起写回。
		weld = contenttpl.InferWeldKind(content)
		// 按改写后的正文钉当前可用工艺，并核作业类型。
		d, err := s.importedProjectDeps(ctx, acc, cur.Level, weld, content)
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
		return store.AssetWrite{Name: live.Name, Content: content, Digest: digest.Sum(content), Copyable: live.Copyable, Status: live.Status, Deps: deps, WeldKind: weld}, nil
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

// importedProjectDeps 按改写后正文钉当前可用工艺，不套模版；焊缝和依赖都要和作业类型一致。
func (s *Assets) importedProjectDeps(ctx context.Context, acc Account, level, weld string, content []byte) ([]AssetDep, error) {
	// 正文里混了几种焊缝就整份拒绝。
	if err := assertProjectWeldKind(weld, content); err != nil {
		return nil, err
	}
	// 从改写后的正文收集工艺引用。
	ids, err := contenttpl.CollectProcessIDsFromItems(contenttpl.SeedProjectItems(), content)
	// 引用收集失败则整份依赖不成立。
	if err != nil {
		return nil, domain.ErrAssetDependency
	}
	// 按本厂或已收的可用工艺钉死当前修订。
	deps, err := mergeProjectDeps(nil, ids, func(id uuid.UUID) (AssetDep, error) {
		// 个人工程还能钉本人的个人工艺
		if level == AssetLevelPersonal {
			// 把引用钉到本人个人工艺或厂级可用工艺。
			return s.pinPersonalProcess(ctx, acc, id)
		}
		// 把引用钉到当前可用的厂级或已收工艺。
		return s.pinFactoryProcess(ctx, id)
	})
	// 钉不住则这条依赖不成立。
	if err != nil {
		return nil, err
	}
	// 个人工程按个人口径核依赖
	if level == AssetLevelPersonal {
		// 依赖不合格则拒绝保存。
		if err := s.assertPersonalProjectDeps(ctx, acc, deps); err != nil {
			return nil, err
		}
		// 厂级工程只能依赖厂级或已收工艺
	} else if err := s.assertFactoryProcessDeps(ctx, deps); err != nil {
		return nil, err
	}
	// 依赖钉到别的作业类型的工艺则整份拒绝。
	if err := s.assertDepsWeldKind(ctx, weld, deps); err != nil {
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

// importBook 同一级未停用的按种类+显示名取最新一条，工艺另全留下按摘要沿用；个人级只看本人的。
func (s *Assets) importBook(ctx context.Context, acc Account, level string) (*importBook, error) {
	// 读出本厂全部原件，后面再按级别和人筛选。
	rows, err := s.store.ListGovernedAssets(ctx)
	// 列表读不出来则整份失败，不回半截。
	if err != nil {
		return nil, err
	}
	// 同名只留先见到的那条。
	book := newImportBook()
	// 只收同一级未停用的；个人级再只看本人的。
	for _, a := range rows {
		// 级别不同或已停用的不参与同名对照。
		if a.Level != level || a.Status == AssetDisabled {
			continue
		}
		// 个人级只和本人自己的比，别人的不碰。
		if level == AssetLevelPersonal && a.CreatorID != acc.ID {
			continue
		}
		// 工艺全留下，同名多条时按摘要挑能沿用的。
		if a.Kind == KindProcess {
			// 记进工艺清单，按摘要沿用时要逐条比。
			book.procs = append(book.procs, a)
		}
		// 用种类加显示名当同名对照，避免工艺和工程混在一起。
		k := importNameKey(a.Kind, a.Name)
		// 同种类同名只留先见到的那一条。
		if _, ok := book.names[k]; ok {
			continue
		}
		// 这个种类加名字还没有对照，就留下。
		book.names[k] = a
	}
	return book, nil
}

// importDigests 原文和按模版规范化后的正文各算一份摘要。
func (s *Assets) importDigests(ctx context.Context, content []byte) [][]byte {
	// 覆盖时存的是原文，先按原文算一份。
	sums := [][]byte{digest.Sum(content)}
	// 新建时会按已收模版套正文，再按套后的算一份。
	norm, err := s.normalizeContent(ctx, KindProcess, content)
	// 套得上且和原文不同，才多比一份摘要。
	if err == nil && !bytes.Equal(norm, content) {
		// 套后正文的摘要也拿来比对。
		sums = append(sums, digest.Sum(norm))
	}
	return sums
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

// importBook 本批同名对照：库里已有的条目，加上本批占用过的身份。
type importBook struct {
	names   map[string]Asset       // 种类+显示名对到最新一条未停用条目
	procs   []Asset                // 库里未停用工艺，按摘要找能沿用的
	claimed map[uuid.UUID]struct{} // 本批新建、覆盖或沿用过的身份
}

// newImportBook 空对照表。
func newImportBook() *importBook {
	return &importBook{names: map[string]Asset{}, claimed: map[uuid.UUID]struct{}{}}
}

// claim 记下本批占用的身份，名字立刻参与后面的同名判断。
func (b *importBook) claim(row Asset) {
	// 名字对到这一条，后面同名能命中。
	rememberImportName(b.names, row)
	// 身份记成本批占用，别的路径不再沿用或覆盖。
	b.claimed[row.ID] = struct{}{}
}

// takenByBatch 这个名字对到的身份已被本批别的路径占用。
func (b *importBook) takenByBatch(kind, name string) bool {
	// 先看名字有没有对到条目。
	cur, ok := b.names[importNameKey(kind, name)]
	// 名字没有条目就不算占用。
	if !ok {
		return false
	}
	// 再看这条身份是不是本批占用的。
	_, ok = b.claimed[cur.ID]
	return ok
}

// batchFreeName 避开本批已占用的名字：先用路径名（带作业类型后缀），仍撞再加序号。
func (b *importBook) batchFreeName(kind, name, rel, suffix string) string {
	// 用去掉扩展名的相对路径当候选新名，拆份的再接后缀。
	label := importPathLabel(rel, suffix)
	// 路径名和原名不同，才考虑拿路径另起。
	if label != "" && label != name {
		// 路径名没被本批占用就用它，撞库里的后面照常处理。
		if !b.takenByBatch(kind, label) {
			return label
		}
		// 路径名也被本批占用了，后面在它上面加序号。
		name = label
	}
	// 仍被本批占用就加序号，直到找出本批没用过的。
	for i := 2; i < 1000; i++ {
		// 加上序号再试一次。
		n := name + " (" + strconv.Itoa(i) + ")"
		// 本批没用过这个名字就用它。
		if !b.takenByBatch(kind, n) {
			return n
		}
	}
	return name
}

// sameContent 在候选名里找摘要和作业类型都相同的可用工艺，本批占用过的不算；want 空不限类型。
func (b *importBook) sameContent(name, rel, want string, sums [][]byte) (Asset, bool) {
	// 候选名：路径名、原名和带作业类型后缀的写法。
	cands := importCandidates(name, rel, want)
	// 记下目前最合适的一条和它的排位。
	best, bestRank := Asset{}, -1
	// 逐条比库里的工艺，挑排位最前的那条。
	for _, p := range b.procs {
		// 本批已经占用的身份不再给别的路径。
		if _, ok := b.claimed[p.ID]; ok {
			continue
		}
		// 草稿或作业类型不同的不沿用，免得钉不上；没人引用的不限类型。
		if p.Status != AssetAvailable || (want != "" && !sameWeldKind(p.WeldKind, want)) {
			continue
		}
		// 名字不在候选里就不比内容。
		r := candidateRank(cands, p.Name)
		// 名字对不上候选，或排位不比已选的靠前，就跳过。
		if r < 0 || (bestRank >= 0 && r >= bestRank) {
			continue
		}
		// 摘要对上任一份，才算内容相同。
		if digestIn(p.Digest, sums) {
			// 记下这条，后面排位更前的才能顶替。
			best, bestRank = p, r
		}
	}
	return best, bestRank >= 0
}

// importCandidates 按路径名、原名、作业类型后缀排出候选名，去重。
func importCandidates(name, rel, want string) []string {
	// 路径名在前，同名不同路径时先认自己那条。
	bases := []string{strings.TrimSpace(legacyPathLabel(rel)), name}
	// 非单层时再加带作业类型后缀的写法。
	if suffix := weldKindSuffix(want); suffix != "" {
		// 两个名字各加一份后缀。
		bases = append(bases, bases[0]+suffix, name+suffix)
	}
	// 去掉空名和重复，排位保持原来的先后。
	out := make([]string, 0, len(bases))
	// 逐个候选去重，空的丢掉。
	for _, n := range bases {
		// 空名或只剩后缀的不当候选。
		if n == "" || n == weldKindSuffix(want) || slices.Contains(out, n) {
			continue
		}
		// 这个候选名还没收过，就留下。
		out = append(out, n)
	}
	return out
}

// candidateRank 名字在候选里的排位，带序号的排在原写法之后；不在候选里回 -1。
func candidateRank(cands []string, name string) int {
	// 原写法直接命中，排位就是下标。
	if i := slices.Index(cands, name); i >= 0 {
		return i
	}
	// 去掉末尾序号后再对一次。
	m := importSerialRe.FindStringSubmatch(name)
	// 末尾没有序号就算不在候选里。
	if m == nil {
		return -1
	}
	// 带序号的变体排在所有原写法之后。
	if i := slices.Index(cands, m[1]); i >= 0 {
		// 排位加上原写法的个数。
		return len(cands) + i
	}
	return -1
}

// digestIn 摘要是否等于其中任一份。
func digestIn(sum []byte, sums [][]byte) bool {
	// 逐份比摘要，对上一份就算相同。
	for _, s := range sums {
		// 摘要相同就算内容相同。
		if bytes.Equal(sum, s) {
			return true
		}
	}
	return false
}

// weldKindSuffix 作业类型拆份时加在名字后的后缀；单层不加。
func weldKindSuffix(kind string) string {
	// 按作业类型给后缀。
	switch kind {
	// 多层那份加多层后缀。
	case store.WeldKindMultilayer:
		return "-多层"
	// T 排那份加 T 排后缀。
	case store.WeldKindTBar:
		return "-T排"
	}
	return ""
}

// sameWeldKind 两个作业类型规范化后是否相同，空的当单层。
func sameWeldKind(a, b string) bool {
	// 先把左边收成规范写法。
	na, errA := store.NormalizeWeldKind(a)
	// 再把右边收成规范写法。
	nb, errB := store.NormalizeWeldKind(b)
	// 有一边不合法就按原样比。
	if errA != nil || errB != nil {
		return a == b
	}
	return na == nb
}

// sortLegacyFiles 按规范化后的相对路径排好，原切片不动。
func sortLegacyFiles(files []LegacyFile) []LegacyFile {
	// 拷一份再排，不改调用方的切片。
	out := slices.Clone(files)
	// 稳定排序，同一路径仍按上传先后。
	sort.SliceStable(out, func(i, j int) bool {
		// 按规范化后的相对路径比先后。
		return normalizeLegacyPath(out[i].Path) < normalizeLegacyPath(out[j].Path)
	})
	return out
}

// normWeldKind 作业类型收成规范写法，空的当单层，不合法的原样留。
func normWeldKind(kind string) string {
	// 收成单层、多层或 T 排。
	n, err := store.NormalizeWeldKind(kind)
	// 不合法就原样留，后面比对自然对不上。
	if err != nil {
		return kind
	}
	return n
}

// legacyWeldOrder 共用工艺拆份时的先后：单层、多层、T 排，第一种留原名。
var legacyWeldOrder = []string{store.WeldKindSingle, store.WeldKindMultilayer, store.WeldKindTBar}

// scanLegacyUses 预扫本批工程，按相对路径记下每份工艺被哪些作业类型引用，按拆份先后排好。
func scanLegacyUses(procs, projects []LegacyFile) map[string][]string {
	// 工艺别名对到相对路径，规则和路径对照相同。
	alias := map[string]string{}
	// 本批会收的工艺相对路径，按排好的顺序。
	var rels []string
	// 同一路径只算一次，和正式收工艺时一致。
	seen := map[string]struct{}{}
	// 按收工艺时的顺序和筛法走一遍，别名后者为准。
	for _, f := range sortLegacyFiles(procs) {
		// 统一斜杠并去掉无意义前缀，空路径不收。
		rel := normalizeLegacyPath(f.Path)
		// 空路径或已见过的路径，正式收时也不会入。
		if _, ok := seen[rel]; rel == "" || ok {
			continue
		}
		// 记下这份路径，后面同路径的不再算。
		seen[rel] = struct{}{}
		// 坏正文正式收时会拒绝，不当引用目标。
		if !json.Valid(f.Content) {
			continue
		}
		// 记下这份工艺的路径，后面认缝隙带要用。
		rels = append(rels, rel)
		// 这份工艺的各种写法都对到它的路径。
		for _, a := range legacyAliases(rel) {
			// 同一写法后收的为准，和路径对照一致。
			alias[a] = rel
		}
	}
	// 每份工艺被引用的作业类型集合。
	used := map[string]map[string]struct{}{}
	// 有 T 排元素没带缝隙带，导入时会按目录补上。
	needBands := false
	// 逐份工程按改写时同样的规则认种类和引用。
	for _, f := range projects {
		// 先解成数组，解不开的整份会被拒绝，不算引用。
		var arr []any
		// 正文不是数组就跳过，改写时同样会拒。
		if json.Unmarshal(f.Content, &arr) != nil {
			continue
		}
		// 按路径判断整份种类，判断不了再逐条推断。
		fileKind := projectFileKind(normalizeLegacyPath(f.Path))
		// 逐条元素认作业类型和引用的工艺。
		for _, el := range arr {
			// 种类和改写时一样：先看路径，再看内容。
			kind := legacyItemKind(fileKind, el)
			// 收出这条元素里的旧路径，和改写时走法相同。
			for _, p := range legacyProcessPaths(el, nil) {
				// 对上本批工艺就记下这种作业类型。
				if target, ok := resolveLegacyAlias(alias, p); ok {
					// 记下这份工艺被这种作业类型引用。
					markLegacyUse(used, target, contenttpl.WeldKindOfItem(kind))
				}
			}
			// T 排元素没带缝隙带或带是空的，导入会按目录补带。
			if obj, _ := el.(map[string]any); obj != nil && tbarNeedsBands(obj, kind) {
				// 记下要补带，收进带里的工艺算 T 排。
				needBands = true
			}
		}
	}
	// 真有 T 排元素要补带时，收进带里的打底、盖面算 T 排。
	if needBands {
		// 逐份记下会收进带里的工艺。
		for _, rel := range legacyBandFiles(rels) {
			// 收进带里的打底、盖面按 T 排引用。
			markLegacyUse(used, rel, store.WeldKindTBar)
		}
	}
	// 按单层、多层、T 排的先后排成列表。
	out := make(map[string][]string, len(used))
	// 逐份工艺排好它用到的作业类型。
	for rel, set := range used {
		// 按固定先后挑出用到的类型。
		for _, k := range legacyWeldOrder {
			// 用到这种类型才排进去。
			if _, ok := set[k]; ok {
				// 按先后接上这种作业类型。
				out[rel] = append(out[rel], k)
			}
		}
	}
	return out
}

// markLegacyUse 记下一份工艺被某种作业类型引用。
func markLegacyUse(used map[string]map[string]struct{}, rel, kind string) {
	// 这份工艺还没有记录就先建集合。
	if used[rel] == nil {
		// 新建这份工艺的作业类型集合。
		used[rel] = map[string]struct{}{}
	}
	// 记下这种作业类型。
	used[rel][kind] = struct{}{}
}

// legacyItemKind 工程元素的种类：整份路径能判断就用它，否则按内容推断。
func legacyItemKind(fileKind string, el any) string {
	// 整份路径判断出了种类就全用它。
	if fileKind != "" {
		return fileKind
	}
	// 路径判断不了，就按这一条的内容推断。
	return contenttpl.InferItemKind(el)
}

// legacyProcessPaths 按改写时的走法收出非空的 processPath，内嵌工艺对象里的不算。
func legacyProcessPaths(v any, out []string) []string {
	// 数组逐个往下，对象里才有旧路径。
	switch x := v.(type) {
	// 数组里每一层都要看。
	case []any:
		// 逐个元素往下收旧路径。
		for _, el := range x {
			// 收这一层的旧路径，接在已有的后面。
			out = legacyProcessPaths(el, out)
		}
	// 对象里才有旧路径。
	case map[string]any:
		// 非空的旧路径收下，空的算没选工艺。
		if s, _ := x["processPath"].(string); s != "" {
			// 记下这条旧路径。
			out = append(out, s)
		}
		// 逐个字段往下，和改写时跳过的一致。
		for k, child := range x {
			// 旧路径和身份字段不再往下。
			if k == "processPath" || k == "processId" {
				continue
			}
			// 内嵌工艺对象改写时会删掉，里面的路径不算。
			if _, isObj := child.(map[string]any); k == "process" && isObj {
				continue
			}
			// 收这一层的旧路径，接在已有的后面。
			out = legacyProcessPaths(child, out)
		}
	}
	return out
}

// resolveLegacyAlias 按路径对照同样的写法顺序，把旧路径对到本批工艺。
func resolveLegacyAlias(alias map[string]string, p string) (string, bool) {
	// 依次试原样、去掉和补上工艺前缀的写法。
	for _, k := range legacyLookupKeys(p) {
		// 对上了就是这份工艺。
		if rel, ok := alias[k]; ok {
			return rel, true
		}
	}
	return "", false
}

// legacyAliases 一份工艺在对照里的写法：原路径、去掉工艺前缀的、无子目录时的文件名。
func legacyAliases(rel string) []string {
	// 去掉工艺前缀的写法也能对上。
	trimmed := strings.TrimPrefix(rel, "processes/")
	// 原路径和去前缀的写法都收。
	out := []string{rel, trimmed}
	// 没有子目录时，文件名也能对上旧路径。
	if !strings.Contains(trimmed, "/") {
		// 补上文件名这种写法。
		out = append(out, path.Base(rel))
	}
	return out
}

// legacyLookupKeys 一条旧路径依次去对照的写法：原样、去掉和补上工艺前缀；空路径没有。
func legacyLookupKeys(p string) []string {
	// 统一斜杠并去掉无意义前缀，空路径算没选。
	p = normalizeLegacyPath(p)
	// 空路径表示没选工艺，不去对照。
	if p == "" {
		return nil
	}
	// 原样在前，再试去掉和补上工艺前缀的写法。
	return []string{p, strings.TrimPrefix(p, "processes/"), "processes/" + p}
}

// legacyGap 缝隙目录里的一份工艺，带上目录给出的层号和间隙。
type legacyGap struct {
	dir      string  // 缝隙目录的相对路径，同目录归一条带
	name     string  // 缝隙目录名
	layer    int     // 从目录名取出的层号
	min, max float64 // 这条带的间隙下限和上限
	role     string  // 打底 root、盖面 cap，别的文件为空
}

// legacyGapFile 认出缝隙目录里的文件；预扫和收带共用，6T1.2 样例目录也算。
func legacyGapFile(rel string) (legacyGap, bool) {
	// 拆出目录和文件，缝隙带按目录归。
	dir, file := path.Split(strings.TrimSuffix(rel, "/"))
	// 去掉尾部斜杠，目录名才能对上缝隙格式。
	dir = strings.TrimSuffix(dir, "/")
	// 只看最后一层目录名是不是层号加间隙。
	base := path.Base(dir)
	// 看目录名是不是层号加间隙，不是就跳过。
	m := gapFolderRe.FindStringSubmatch(base)
	// 目录名不是层号加间隙的格式就不算缝隙目录。
	if m == nil {
		return legacyGap{}, false
	}
	// 不是 T 排目录时，只留那个固定间隙样例。
	if !strings.Contains(dir, "T排立对接") && !strings.Contains(strings.ToLower(dir), "tbar") && !strings.Contains(dir, "6T1.2") {
		return legacyGap{}, false
	}
	// 从目录名取出层号，排缝隙带要用。
	layer, _ := strconv.Atoi(m[1])
	// 从目录名取出这条带的间隙下限。
	min, _ := strconv.ParseFloat(m[2], 64)
	// 从目录名取出这条带的间隙上限。
	max, _ := strconv.ParseFloat(m[3], 64)
	// 先按目录记下这份文件，打底盖面下面再认。
	g := legacyGap{dir: dir, name: base, layer: layer, min: min, max: max}
	// 去掉扩展名后看是打底还是盖面。
	stem := strings.TrimSuffix(file, path.Ext(file))
	// 按文件名把工艺归到打底或盖面。
	switch {
	// 名字里有打底，就当成这条带的打底工艺。
	case strings.Contains(stem, "打底"):
		// 记成打底。
		g.role = "root"
	// 名字里有盖面，就当成这条带的盖面工艺。
	case strings.Contains(stem, "盖面"):
		// 记成盖面。
		g.role = "cap"
	}
	return g, true
}

// legacyBandFiles 挑出会收进缝隙带的打底和盖面；同目录同角色以后收的为准。
func legacyBandFiles(rels []string) []string {
	// 目录加角色对到最后一份文件。
	picked := map[string]string{}
	// 按收工艺的顺序走，后收的顶替前面的。
	for _, rel := range rels {
		// 认缝隙目录里的打底或盖面。
		g, ok := legacyGapFile(rel)
		// 不在缝隙目录或不是打底盖面就跳过。
		if !ok || g.role == "" {
			continue
		}
		// 同目录同角色只留后收的那份。
		picked[g.dir+"\n"+g.role] = rel
	}
	// 收成列表交回。
	out := make([]string, 0, len(picked))
	// 逐条收下挑中的文件。
	for _, rel := range picked {
		// 这份会收进缝隙带。
		out = append(out, rel)
	}
	return out
}

// 导入路径加作业类型到已入库工艺的对照，含去掉前缀的别名。
type pathIndex struct {
	byPath map[string]uuid.UUID // 作业类型加相对路径对应的工艺身份
	files  []legacyMapped       // 原始相对路径，用来收间隙带，不含别名
}

// 一份已入库旧工艺的原始路径、作业类型和身份。
type legacyMapped struct {
	Path string    // 导入时的相对路径
	Kind string    // 这一份的作业类型
	ID   uuid.UUID // 入库后的工艺身份
}

// newPathIndex 空对照表。
func newPathIndex() *pathIndex {
	return &pathIndex{byPath: map[string]uuid.UUID{}}
}

// pathKey 对照键：作业类型加路径写法，同一路径各类型各一份。
func pathKey(kind, p string) string {
	// 作业类型收成规范写法再接路径，空的当单层。
	return normWeldKind(kind) + "\n" + p
}

// add 按作业类型记下相对路径及其不含 processes/ 前缀的别名。
func (idx *pathIndex) add(rel, kind string, id uuid.UUID) {
	// 留下原始路径，后面收缝隙带时不用别名。
	idx.files = append(idx.files, legacyMapped{Path: rel, Kind: normWeldKind(kind), ID: id})
	// 各种写法都对到这一份身份。
	for _, a := range legacyAliases(rel) {
		// 这种写法加作业类型对到入库后的身份。
		idx.byPath[pathKey(kind, a)] = id
	}
}

// lookup 按作业类型取对应那份；空路径算未选，对不上则失败。
func (idx *pathIndex) lookup(p, kind string) (uuid.UUID, bool) {
	// 依次试原样、去掉和补上工艺前缀的写法。
	keys := legacyLookupKeys(p)
	// 空路径表示没选工艺，不算失败。
	if len(keys) == 0 {
		return uuid.Nil, true
	}
	// 逐个写法去对照，对上就用。
	for _, k := range keys {
		// 这种写法在这种作业类型下对上了已入库工艺。
		if id, ok := idx.byPath[pathKey(kind, k)]; ok {
			return id, true
		}
	}
	return uuid.Nil, false
}

// gapBands 按缝隙目录收 T 排那份打底/盖面引用；缺文件的带仍可留下空引用。
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
	// 只处理缝隙目录里 T 排那份打底和盖面。
	for _, item := range idx.files {
		// 别的作业类型那份不进缝隙带。
		if item.Kind != store.WeldKindTBar {
			continue
		}
		// 用原始相对路径认缝隙目录，不用别名。
		g, ok := legacyGapFile(item.Path)
		// 不在缝隙目录里就跳过，不收成带。
		if !ok {
			continue
		}
		// 同一缝隙目录收成一条带。
		f := folders[g.dir]
		// 这条目录还没有带，下面按层号和间隙新建。
		if f == nil {
			// 这条目录还没有带，按层号和间隙新建。
			f = &folder{name: g.name, layer: g.layer, min: g.min, max: g.max}
			// 按目录记下这条缝隙带。
			folders[g.dir] = f
		}
		// 按文件名把工艺归到打底或盖面。
		switch g.role {
		// 打底文件就当成这条带的打底工艺。
		case "root":
			// 文件名里有打底，就把这份工艺当成打底。
			f.root = item.ID
		// 盖面文件就当成这条带的盖面工艺。
		case "cap":
			// 文件名里有盖面，就把这份工艺当成盖面。
			f.cap = item.ID
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

// rewriteLegacyProject 逐条按作业类型把 processPath 换成身份，删掉内嵌工艺对象，并打上模版身份。
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
	// 逐条认种类、换引用并打上模版身份。
	for _, el := range arr {
		// 种类先看整份路径，判断不了再按这一条的内容推断。
		kind := legacyItemKind(fileKind, el)
		// 按这一条的作业类型取对应那份工艺，对不上整份拒绝。
		if err := rewriteRefs(el, idx, contenttpl.WeldKindOfItem(kind)); err != nil {
			return nil, err
		}
		// 不是对象的元素跳过，不拿来打模版。
		obj, _ := el.(map[string]any)
		// 不是对象的元素跳过，不拿来打模版。
		if obj == nil {
			continue
		}
		// T 排元素缺间隙带又无带可补，入库也焊不了，整份拒绝。
		if tbarNeedsBands(obj, kind) && len(bands) == 0 {
			return nil, errTBarGapBandsMissing
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

// tbarNeedsBands 这条是 T 排且正文没带或只带了空的间隙带，要靠本批缝隙目录补。
func tbarNeedsBands(obj map[string]any, kind string) bool {
	// 不是 T 排就不需要间隙带。
	if kind != contenttpl.ItemTBar {
		return false
	}
	// 正文自带非空间隙带的照旧，不靠目录补。
	return !hasGapBands(obj)
}

// hasGapBands 正文至少带一条间隙带才算自带；空数组和没这个键一样要补。
func hasGapBands(obj map[string]any) bool {
	// 取出间隙带，不是数组的按没带算。
	arr, _ := obj["gapBands"].([]any)
	// 至少一条才算自带，空数组照样要补。
	return len(arr) > 0
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
		// 正文没带或只带了空的间隙带时，用目录里收来的补上。
		if !hasGapBands(obj) {
			// 用本批缝隙目录收来的间隙带补上。
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

// rewriteRefs 非空 processPath 必须能对上这种作业类型那份已入工艺，否则整份工程拒绝。
func rewriteRefs(v any, idx *pathIndex, kind string) error {
	// 数组继续往下，对象里才改工艺路径。
	switch x := v.(type) {
	// 数组里每一层都要改写，一处失败整份拒绝。
	case []any:
		// 数组里的每一层都要改写，一处失败整份拒绝。
		for _, el := range x {
			// 这一步失败则停下，避免带着残缺结果继续。
			if err := rewriteRefs(el, idx, kind); err != nil {
				return err
			}
		}
	// 对象里才有工艺路径，对不上就整份拒绝。
	case map[string]any:
		// 有旧路径就必须对上已入库工艺，否则整份拒绝。
		if raw, ok := x["processPath"]; ok {
			// 路径不是字符串就当没选工艺。
			s, _ := raw.(string)
			// 把正文里的引用钉到这种作业类型那份已入库工艺。
			id, found := idx.lookup(s, kind)
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
			if err := rewriteRefs(child, idx, kind); err != nil {
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

// uniqueImportName 同名按相对路径（带作业类型后缀）另起；仍撞则加序号。
func uniqueImportName(names map[string]Asset, kind, name, rel, suffix string) string {
	// 用去掉扩展名的相对路径当候选新名，拆份的再接后缀。
	label := importPathLabel(rel, suffix)
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

// importPathLabel 按路径另起的名字；非首种作业类型那份再接后缀。
func importPathLabel(rel, suffix string) string {
	// 用去掉扩展名的相对路径当名字。
	label := strings.TrimSpace(legacyPathLabel(rel))
	// 没有路径名就不起，也不单留一个后缀。
	if label == "" {
		return ""
	}
	return label + suffix
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
