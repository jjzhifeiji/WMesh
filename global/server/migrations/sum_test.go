// 迁移清单门禁：已登记的迁移不能改、不能删，新迁移要登记进 migrations.sum。
package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

// sumFile 迁移清单，和脚本放在同一目录，随代码一起评审。
const sumFile = "migrations.sum"

// updateEnv 设成 1 时清单测试改为把新迁移追加进清单。
const updateEnv = "WMESH_UPDATE_MIGRATIONS_SUM"

// totalKey 清单末行的键，后面跟前面各行的总摘要。
const totalKey = "total"

// updateHint 新迁移没进清单时提示的更新命令。
const updateHint = "执行 make migrations-sum（即在 server 目录 WMESH_UPDATE_MIGRATIONS_SUM=1 go test ./migrations/）"

// sumHex 清单里的摘要写法：64 位小写十六进制。
var sumHex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// sumEntry 清单的一行：迁移文件名与其内容摘要。
type sumEntry struct {
	name string // 迁移文件名
	hash string // 文件内容的 sha256 十六进制
}

// TestMigrationsSum 默认核对清单；设了更新开关才追加新迁移。
func TestMigrationsSum(t *testing.T) {
	// 默认只核对，不写文件。
	check := verifySum
	// 开发者显式要求时才写清单。
	if os.Getenv(updateEnv) == "1" {
		// 换成追加模式。
		check = updateSum
	}
	// 迁移目录就是本包目录，go test 在这里跑。
	for _, p := range check(os.DirFS("."), sumFile) {
		// 每条清单问题单独报。
		t.Error(p)
	}
}

// digestFiles 算出目录里每个 .sql 的摘要，按文件名排序。
func digestFiles(fsys fs.FS) ([]sumEntry, error) {
	// 和规则核对用同一份文件清单。
	names, err := sqlNames(fsys)
	// 列不出文件就算不了。
	if err != nil {
		return nil, err
	}
	// 每个文件一条。
	entries := make([]sumEntry, 0, len(names))
	// 逐个算摘要。
	for _, name := range names {
		// 按原始字节算，与 goose 读到的一致。
		data, err := fs.ReadFile(fsys, name)
		// 读不了就整份作废。
		if err != nil {
			return nil, err
		}
		// 收下文件名和摘要。
		entries = append(entries, sumEntry{name: name, hash: digestText(string(data))})
	}
	return entries, nil
}

// digestText 算一段内容的 sha256 十六进制。
func digestText(s string) string {
	// 算出摘要。
	sum := sha256.Sum256([]byte(s))
	// 转成小写十六进制。
	return hex.EncodeToString(sum[:])
}

// entryLines 把条目拼成清单正文，每行「文件名 摘要」。
func entryLines(entries []sumEntry) string {
	// 逐行拼接。
	var b strings.Builder
	// 每个条目一行。
	for _, e := range entries {
		// 写一行，结尾换行。
		b.WriteString(e.name + " " + e.hash + "\n")
	}
	// 交回拼好的正文。
	return b.String()
}

// formatSum 写成清单全文：各行之后补一行 total 加前面各行的总摘要。
func formatSum(entries []sumEntry) []byte {
	// 先拼出各行。
	body := entryLines(entries)
	// 末行总摘要盖住前面全部内容，合并冲突时一眼可见。
	return []byte(body + totalKey + " " + digestText(body) + "\n")
}

// parseSum 拆出清单各行与末行总摘要；格式不对、未按名排序或重名都报错。
func parseSum(data []byte) ([]sumEntry, string, error) {
	// 按文本处理。
	text := string(data)
	// 清单由工具写出，结尾必有换行。
	if !strings.HasSuffix(text, "\n") {
		// 交回格式错误。
		return nil, "", errors.New("末尾缺换行")
	}
	// 去掉最后的换行再按行拆。
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	// 末行要是 total 加摘要。
	total, ok := strings.CutPrefix(lines[len(lines)-1], totalKey+" ")
	// 末行不对就整份不认。
	if !ok || !sumHex.MatchString(total) {
		// 交回格式错误。
		return nil, "", errors.New("末行要写 total 加总摘要")
	}
	// 其余每行一个条目。
	entries := make([]sumEntry, 0, len(lines)-1)
	// 逐行拆出文件名和摘要。
	for i, line := range lines[:len(lines)-1] {
		// 文件名与摘要之间一个空格。
		name, hash, ok := strings.Cut(line, " ")
		// 写法不对就整份不认。
		if !ok || name == "" || !sumHex.MatchString(hash) {
			// 交回出错的行号。
			return nil, "", fmt.Errorf("第 %d 行要写「文件名 摘要」", i+1)
		}
		// 必须严格按文件名递增，重名也算错。
		if len(entries) > 0 && name <= entries[len(entries)-1].name {
			// 交回出错的行号和文件名。
			return nil, "", fmt.Errorf("第 %d 行 %s 没按文件名排序或重名", i+1, name)
		}
		// 收下这个条目。
		entries = append(entries, sumEntry{name: name, hash: hash})
	}
	return entries, total, nil
}

// diffSum 对比清单与目录：已登记的改了或没了记为违规，没登记的新文件另行交回。
func diffSum(recorded, current []sumEntry) ([]string, []sumEntry) {
	// 目录里现有文件的摘要，按名查。
	hashes := map[string]string{}
	// 逐个登记现有文件。
	for _, e := range current {
		// 记下这个文件的摘要。
		hashes[e.name] = e.hash
	}
	// 收集违规。
	var problems []string
	// 清单里登记过的文件名。
	known := map[string]bool{}
	// 逐条核对已登记的迁移。
	for _, e := range recorded {
		// 记下已登记。
		known[e.name] = true
		// 取目录里同名文件的摘要。
		hash, ok := hashes[e.name]
		// 按有无与是否一致分情况。
		switch {
		// 文件没了：已上线的迁移删了或改了名。
		case !ok:
			// 记下缺文件。
			problems = append(problems, fmt.Sprintf("清单里的 %s 在目录里找不到：已上线的迁移不能删除或改名", e.name))
		// 内容变了：已上线的迁移被改过。
		case hash != e.hash:
			// 记下被改的文件。
			problems = append(problems, fmt.Sprintf("%s 内容和清单不符：已上线的迁移不能改，请新增迁移文件", e.name))
		}
	}
	// 收集没登记的新文件。
	var added []sumEntry
	// 目录里有、清单里没有的就是新迁移。
	for _, e := range current {
		// 没登记过才算新的。
		if !known[e.name] {
			// 收下这个新迁移。
			added = append(added, e)
		}
	}
	return problems, added
}

// verifySum 按清单核对目录：已登记的不能改、不能少，新迁移要先登记。
func verifySum(fsys fs.FS, sumPath string) []string {
	// 读出清单。
	data, err := os.ReadFile(sumPath)
	// 清单不存在要先生成。
	if errors.Is(err, fs.ErrNotExist) {
		// 交回生成命令。
		return []string{"缺迁移清单 " + sumFile + "：" + updateHint}
	}
	// 其余读失败照报。
	if err != nil {
		// 交回读失败原因。
		return []string{fmt.Sprintf("读迁移清单失败：%v", err)}
	}
	// 拆出各行与总摘要。
	recorded, total, err := parseSum(data)
	// 格式不对就没法核。
	if err != nil {
		// 交回格式问题。
		return []string{fmt.Sprintf("迁移清单格式不对：%v", err)}
	}
	// 总摘要对不上说明清单被手改过或合并没处理好。
	if total != digestText(entryLines(recorded)) {
		// 交回总摘要问题。
		return []string{"迁移清单末行总摘要对不上：清单被手改过或合并没处理好，恢复后" + updateHint}
	}
	// 算出目录里各文件的摘要。
	current, err := digestFiles(fsys)
	// 算不了就没法核。
	if err != nil {
		// 交回读文件失败原因。
		return []string{fmt.Sprintf("读迁移文件失败：%v", err)}
	}
	// 对出改动、缺失与新文件。
	problems, added := diffSum(recorded, current)
	// 新文件逐个提示登记。
	for _, e := range added {
		// 记下没登记的新迁移。
		problems = append(problems, fmt.Sprintf("新迁移 %s 还没登记进清单：%s", e.name, updateHint))
	}
	return problems
}

// updateSum 把新迁移追加进清单，清单不存在时整份写出；改了或删了已登记的迁移照样报错且不写。
func updateSum(fsys fs.FS, sumPath string) []string {
	// 规则没过的文件不登记，免得登记后再改就成了改旧迁移。
	if problems := checkRules(fsys); len(problems) > 0 {
		// 交回规则问题，清单不动。
		return append([]string{"迁移规则没过，先改好再更新清单："}, problems...)
	}
	// 算出目录里各文件的摘要。
	current, err := digestFiles(fsys)
	// 算不了就不写。
	if err != nil {
		// 交回读文件失败原因。
		return []string{fmt.Sprintf("读迁移文件失败：%v", err)}
	}
	// 读出旧清单。
	data, err := os.ReadFile(sumPath)
	// 首次生成时整份写出。
	if errors.Is(err, fs.ErrNotExist) {
		// 写出全部条目。
		return writeSum(sumPath, current)
	}
	// 其余读失败照报。
	if err != nil {
		// 交回读失败原因。
		return []string{fmt.Sprintf("读迁移清单失败：%v", err)}
	}
	// 旧总摘要不核，写出时按条目重算；条目本身逐条核对。
	recorded, _, err := parseSum(data)
	// 格式不对就不写。
	if err != nil {
		// 交回格式问题。
		return []string{fmt.Sprintf("迁移清单格式不对：%v", err)}
	}
	// 对出改动、缺失与新文件。
	problems, added := diffSum(recorded, current)
	// 已登记的改了或没了，不能借更新命令洗白。
	if len(problems) > 0 {
		return problems
	}
	// 只在末尾追加新条目，旧条目原样保留。
	return writeSum(sumPath, append(recorded, added...))
}

// writeSum 写出清单；写失败就报。
func writeSum(path string, entries []sumEntry) []string {
	// 整份覆盖写出。
	if err := os.WriteFile(path, formatSum(entries), 0o644); err != nil {
		// 交回写失败原因。
		return []string{fmt.Sprintf("写迁移清单失败：%v", err)}
	}
	return nil
}
