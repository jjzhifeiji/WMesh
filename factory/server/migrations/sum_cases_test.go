// 迁移清单门禁的反例与正例：用内存目录和临时清单，确认改旧、删旧、漏登记都会拦，更新只追加。
package migrations

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// sampleFS 一套守规则的两支迁移，清单用例拿它当底子。
func sampleFS() fstest.MapFS {
	// 第一支建表，第二支加列。
	return fstest.MapFS{
		"0001_init.up.sql": upFile(goodTable),
		"0002_more.up.sql": upFile("ALTER TABLE t ADD COLUMN note TEXT;\nCOMMENT ON COLUMN t.note IS '备注';"),
	}
}

// tempSum 本用例专用的清单路径，用完自动清掉。
func tempSum(t *testing.T) string {
	// 失败行指到调用用例。
	t.Helper()
	// 放在临时目录里，不碰仓里的清单。
	return filepath.Join(t.TempDir(), sumFile)
}

// mustRead 读出文件全文，读不了就停。
func mustRead(t *testing.T, path string) string {
	// 失败行指到调用用例。
	t.Helper()
	// 读出全文。
	data, err := os.ReadFile(path)
	// 读不了后面的断言没有底数。
	if err != nil {
		// 停住本用例。
		t.Fatal(err)
	}
	// 交回全文。
	return string(data)
}

// wantNone 问题清单要为空。
func wantNone(t *testing.T, problems []string) {
	// 失败行指到调用用例。
	t.Helper()
	// 有任何问题都算失败。
	if len(problems) > 0 {
		// 打出全部问题。
		t.Fatalf("不该有问题：%v", problems)
	}
}

// wantProblem 问题清单里要有一条含给定字样。
func wantProblem(t *testing.T, problems []string, want string) {
	// 失败行指到调用用例。
	t.Helper()
	// 合起来查字样。
	if !strings.Contains(strings.Join(problems, "\n"), want) {
		// 打出实际问题，方便对照。
		t.Fatalf("应报 %q，实际：%v", want, problems)
	}
}

// TestSumFirstWrite 清单不存在时整份写出，写出后核对通过，末行是总摘要。
func TestSumFirstWrite(t *testing.T) {
	// 准备临时清单路径。
	path := tempSum(t)
	// 首次更新整份写出。
	wantNone(t, updateSum(sampleFS(), path))
	// 写出后立刻核对应通过。
	wantNone(t, verifySum(sampleFS(), path))
	// 按行拆开看格式。
	lines := strings.Split(strings.TrimSuffix(mustRead(t, path), "\n"), "\n")
	// 两支迁移各一行，再加末行总摘要。
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "0001_init.up.sql ") || !strings.HasPrefix(lines[2], totalKey+" ") {
		// 打出实际内容。
		t.Fatalf("清单格式不对：%q", lines)
	}
}

// TestSumMissing 仓里没有清单时核对失败，并提示生成命令。
func TestSumMissing(t *testing.T) {
	// 清单路径存在但文件没写。
	wantProblem(t, verifySum(sampleFS(), tempSum(t)), "make migrations-sum")
}

// TestSumRejectsEditedMigration 已登记的迁移改了内容：核对失败，更新也不写。
func TestSumRejectsEditedMigration(t *testing.T) {
	// 准备临时清单路径。
	path := tempSum(t)
	// 准备迁移目录。
	files := sampleFS()
	// 先登记现有两支。
	wantNone(t, updateSum(files, path))
	// 记下登记后的清单。
	before := mustRead(t, path)
	// 改动已登记的第一支。
	files["0001_init.up.sql"] = upFile(goodTable + "\nSELECT 1;")
	// 核对要拦下改旧迁移。
	wantProblem(t, verifySum(files, path), "0001_init.up.sql 内容和清单不符：已上线的迁移不能改，请新增迁移文件")
	// 更新命令同样拦下。
	wantProblem(t, updateSum(files, path), "已上线的迁移不能改")
	// 清单不能被洗白。
	if after := mustRead(t, path); after != before {
		// 打出被改后的清单。
		t.Fatalf("改旧迁移后清单被改写：%q", after)
	}
}

// TestSumRejectsRemovedMigration 已登记的迁移删了或改了名：核对失败，更新也不写。
func TestSumRejectsRemovedMigration(t *testing.T) {
	// 准备临时清单路径。
	path := tempSum(t)
	// 准备迁移目录。
	files := sampleFS()
	// 先登记现有两支。
	wantNone(t, updateSum(files, path))
	// 记下登记后的清单。
	before := mustRead(t, path)
	// 删掉已登记的第二支。
	delete(files, "0002_more.up.sql")
	// 核对要拦下删旧迁移。
	wantProblem(t, verifySum(files, path), "0002_more.up.sql 在目录里找不到")
	// 更新命令同样拦下。
	wantProblem(t, updateSum(files, path), "不能删除或改名")
	// 清单不能被改写。
	if after := mustRead(t, path); after != before {
		// 打出被改后的清单。
		t.Fatalf("删旧迁移后清单被改写：%q", after)
	}
}

// TestSumAppendsNewMigration 新迁移没登记时核对失败；更新只在末尾追加，旧条目原样。
func TestSumAppendsNewMigration(t *testing.T) {
	// 准备临时清单路径。
	path := tempSum(t)
	// 准备迁移目录。
	files := sampleFS()
	// 先拿走第二支，只登记第一支。
	second := files["0002_more.up.sql"]
	// 目录里暂时只留第一支。
	delete(files, "0002_more.up.sql")
	// 登记第一支。
	wantNone(t, updateSum(files, path))
	// 旧清单去掉末行总摘要，剩下的是旧条目。
	oldEntries := strings.SplitAfter(mustRead(t, path), "\n")[0]
	// 新迁移放回目录。
	files["0002_more.up.sql"] = second
	// 核对要提示登记新迁移。
	wantProblem(t, verifySum(files, path), "新迁移 0002_more.up.sql 还没登记进清单")
	// 更新命令追加新条目。
	wantNone(t, updateSum(files, path))
	// 旧条目要原样排在最前。
	if after := mustRead(t, path); !strings.HasPrefix(after, oldEntries) || !strings.Contains(after, "\n0002_more.up.sql ") {
		// 打出更新后的清单。
		t.Fatalf("更新没按追加写：%q", after)
	}
	// 追加后核对通过。
	wantNone(t, verifySum(files, path))
}

// TestSumRejectsHandEdit 手改了清单（总摘要对不上或格式乱了）时核对失败。
func TestSumRejectsHandEdit(t *testing.T) {
	// 准备临时清单路径。
	path := tempSum(t)
	// 先正常写出清单。
	wantNone(t, updateSum(sampleFS(), path))
	// 读出清单准备手改。
	text := mustRead(t, path)
	// 把末行总摘要换成别的值。
	forged := text[:strings.LastIndex(text, totalKey+" ")] + totalKey + " " + strings.Repeat("0", 64) + "\n"
	// 写回手改后的清单。
	if err := os.WriteFile(path, []byte(forged), 0o644); err != nil {
		// 写不了就停。
		t.Fatal(err)
	}
	// 核对要拦下总摘要不符。
	wantProblem(t, verifySum(sampleFS(), path), "总摘要对不上")
	// 两行对调，顺序乱了。
	lines := strings.Split(text, "\n")
	// 第一、二行互换。
	lines[0], lines[1] = lines[1], lines[0]
	// 写回顺序乱了的清单。
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		// 写不了就停。
		t.Fatal(err)
	}
	// 核对要拦下格式问题。
	wantProblem(t, verifySum(sampleFS(), path), "没按文件名排序")
}

// TestSumUpdateRefusesBadRules 规则没过的迁移不登记，清单也不生成。
func TestSumUpdateRefusesBadRules(t *testing.T) {
	// 准备临时清单路径。
	path := tempSum(t)
	// 一支缺 Up 注解的迁移。
	files := fstest.MapFS{"0001_init.up.sql": rawFile(goodTable)}
	// 更新命令要先报规则问题。
	wantProblem(t, updateSum(files, path), "迁移规则没过")
	// 清单不能被写出。
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		// 打出实际状态。
		t.Fatalf("规则没过却写了清单：%v", err)
	}
}
