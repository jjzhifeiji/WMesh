// 探活带各厂库迁移版本与打开失败退避；个别厂迁移失败不拖累整体状态。
package httpapi_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
	"wmesh/factory/internal/platform/id"
	"wmesh/factory/internal/platform/provision"
	"wmesh/factory/internal/platform/testpg"
	"wmesh/factory/migrations"
)

// 一家厂库迁移失败：探活仍回 200 和 ok，失败那家只报文件名和重试时间，好的那家报最新版本。
func TestHealthzSchema(t *testing.T) {
	// 连上测试库并建枢纽，连不上就停测。
	h, err := hub.New(testpg.AdminDSN())
	// 测试库连不上则停测。
	if err != nil {
		// 与预期不符就停测，避免后面连环误判。
		t.Fatalf("hub: %v", err)
	}
	// 坏的那家和好的那家各一个身份。
	bad, good := id.New(), id.New()
	// 测完删掉两家厂库并关枢纽。
	t.Cleanup(func() {
		// 删掉坏的那家。
		_ = h.Drop(bad)
		// 删掉好的那家。
		_ = h.Drop(good)
		// 关掉枢纽，释放测试占用的连接。
		h.Close()
	})
	// 拉起临时服务，测完由清理关掉。
	srv := httptest.NewServer(httpapi.New(h, "boot-secret", "").Router())
	// 测完关闭临时服务，把端口让出来。
	t.Cleanup(srv.Close)
	// 建出坏库：先占掉 people 表，第一支迁移必失败。
	breakDB(t, bad)
	// 用共享密码建好的那家，库随之打开。
	if code, body := do(t, srv, "POST", "/internal/bootstrap", "boot-secret", `{"factoryId":"`+good.String()+`","saLogin":"sa","saDisplay":"超管"}`); code != http.StatusCreated {
		// 建厂失败就停测。
		t.Fatalf("bootstrap %d %s", code, body)
	}
	// 访问坏的那家，迁移失败。
	if _, err := h.Service(context.Background(), bad); err == nil {
		// 坏库居然打开了，前提不成立。
		t.Fatal("broken factory opened")
	}
	// 记下探活前的时间，重试点应在它之后。
	before := time.Now()
	// 探活。
	code, body := do(t, srv, "GET", "/healthz", "", "")
	// 个别厂失败不改状态码和整体状态。
	if code != http.StatusOK || gjson(t, body, "status") != "ok" || gjson(t, body, "db") != "ok" {
		// 把探活结果打出来。
		t.Fatalf("healthz %d %s", code, body)
	}
	// 库给的原错误不该出现在探活里。
	if strings.Contains(body, "already exists") || strings.Contains(body, "SQLSTATE") {
		// 原错误外露就停测。
		t.Fatalf("healthz leaks db error: %s", body)
	}
	// 准备按结构读出 schema 一段。
	var resp struct {
		// 探活里的厂库结构一段。
		Schema hub.SchemaState `json:"schema"`
	}
	// 读不出就停测。
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		// 把回包打出来便于对照。
		t.Fatalf("json %s: %v", body, err)
	}
	// 准备找坏的那家。
	var found bool
	// 逐条看失败列表。
	for _, f := range resp.Schema.Failing {
		// 只看坏的那家。
		if f.FactoryID != bad {
			continue
		}
		// 记下找到了。
		found = true
		// 原因、文件和重试时间都要对。
		if f.Reason != "migrate" || f.File != "0001_init.up.sql" || !f.RetryAt.After(before) {
			// 把这一条打出来。
			t.Fatalf("failing %+v", f)
		}
	}
	// 没列出坏的那家就停测。
	if !found {
		// 把探活结果打出来。
		t.Fatalf("bad factory not listed: %s", body)
	}
	// 好的那家应报到最新版本。
	want := latestVersion(t)
	// 准备找好的那家。
	found = false
	// 逐条看已打开列表。
	for _, f := range resp.Schema.Factories {
		// 只看好的那家。
		if f.FactoryID != good {
			continue
		}
		// 记下找到了。
		found = true
		// 版本对不上就停测。
		if f.Version != want {
			// 把这一条和应有版本打出来。
			t.Fatalf("factory %+v, want version %d", f, want)
		}
	}
	// 没列出好的那家就停测。
	if !found {
		// 把探活结果打出来。
		t.Fatalf("good factory not listed: %s", body)
	}
}

// breakDB 建出这家厂的库并先占掉 people 表，第一支迁移必撞表已存在。
func breakDB(t *testing.T, fid uuid.UUID) {
	// 标成辅助函数，失败算到调用方。
	t.Helper()
	// 连维护库好建厂库。
	admin := testpg.Open(t)
	// 按工厂身份算出库名。
	name := provision.DBName(fid)
	// 建不出厂库就停测。
	if err := provision.Ensure(admin, name); err != nil {
		// 把建库错误打出来并停住。
		t.Fatalf("create db: %v", err)
	}
	// 拼出这家厂库的连接串。
	dsn, err := provision.SwapDB(testpg.AdminDSN(), name)
	// 拼不出就停测。
	if err != nil {
		// 把拼串错误打出来并停住。
		t.Fatalf("dsn: %v", err)
	}
	// 直连这家厂库，日志关掉免得刷屏。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 连不上就停测。
	if err != nil {
		// 把连库错误打出来并停住。
		t.Fatalf("open: %v", err)
	}
	// 用完就关这条直连，删库前不占着。
	defer func() {
		// 取出底层连接池，能取到才关。
		if sqlDB, err := db.DB(); err == nil {
			// 关掉直连池。
			_ = sqlDB.Close()
		}
	}()
	// 先占掉 people 表，0001 建表时必失败。
	if err := db.Exec("CREATE TABLE people (x INT)").Error; err != nil {
		// 占不了表就停测，前提不成立。
		t.Fatalf("break db: %v", err)
	}
}

// latestVersion 算出嵌进程序的最大迁移版本，迁完厂库应到这一版。
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
