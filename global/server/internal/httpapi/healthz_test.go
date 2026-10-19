// 探活带 WAN 库迁移版本；读不出版本只标 -1，不拖累整体状态。
package httpapi_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"wmesh/global/internal/httpapi"
	"wmesh/global/internal/platform/testpg"
	"wmesh/global/internal/service"
	"wmesh/global/internal/store"
	"wmesh/global/migrations"
)

// 迁好的库报最新版本；版本表没了探活仍回 200 和 ok，只把版本标 -1。
func TestHealthzSchema(t *testing.T) {
	// 建一块迁好的临时库，测完自动删。
	db := testpg.Fresh(t)
	// 按这块库组装服务。
	svc := service.NewService(store.Open(db))
	// 起临时云端，只在本测里访问。
	srv := httptest.NewServer(httpapi.New(svc, "test").Router())
	// 测完关掉临时服务，避免端口占着。
	t.Cleanup(srv.Close)
	// 探活。
	code, body := do(t, srv, "GET", "/healthz", "", "")
	// 状态码、整体状态或版本不对就停测。
	if code != http.StatusOK || gjson(t, body, "status") != "ok" || gjson(t, body, "schema") != strconv.FormatInt(latestVersion(t), 10) {
		// 把探活结果打出来。
		t.Fatalf("healthz %d %s", code, body)
	}
	// 去掉版本表，模拟版本读不出。
	if err := db.Exec("DROP TABLE goose_db_version").Error; err != nil {
		// 去不掉就停测，前提不成立。
		t.Fatalf("drop version table: %v", err)
	}
	// 再探活。
	code, body = do(t, srv, "GET", "/healthz", "", "")
	// 读不出版本只标 -1，状态码和整体状态不变。
	if code != http.StatusOK || gjson(t, body, "status") != "ok" || gjson(t, body, "db") != "ok" || gjson(t, body, "schema") != "-1" {
		// 把探活结果打出来。
		t.Fatalf("healthz without version table %d %s", code, body)
	}
	// 库给的原错误不该出现在探活里。
	if strings.Contains(body, "goose_db_version") || strings.Contains(body, "SQLSTATE") {
		// 原错误外露就停测。
		t.Fatalf("healthz leaks db error: %s", body)
	}
}

// latestVersion 算出嵌进程序的最大迁移版本，迁完 WAN 库应到这一版。
func latestVersion(t *testing.T) int64 {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 列出全部迁移脚本。
	names, err := fs.Glob(migrations.FS, "*.sql")
	// 列不出就停测。
	if err != nil {
		// 把列脚本的错误打出来并停住。
		t.Fatal(err)
	}
	// 准备记最大版本。
	var top int64
	// 逐个取文件名开头数字。
	for _, name := range names {
		// 取下划线前的数字当版本。
		v, err := strconv.ParseInt(name[:strings.IndexByte(name, '_')], 10, 64)
		// 文件名开头不是数字就停测。
		if err != nil {
			// 把坏文件名打出来并停住。
			t.Fatalf("name %s: %v", name, err)
		}
		// 更大就换上。
		if v > top {
			// 记下更大的版本。
			top = v
		}
	}
	return top
}
