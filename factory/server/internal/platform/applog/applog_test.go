// JSON 信封字段与级别开关。
package applog_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"wmesh/factory/internal/platform/applog"
)

// 核对日志里时间、级别和正文都在。
func TestJSONEnvelope(t *testing.T) {
	// 钉住这一个环境值。
	t.Setenv("WMESH_LOG_LEVEL", "info")
	// 准备一块缓冲把输出接住，方便事后检查。
	var buf bytes.Buffer
	// 新建这一份后面要用的对象。
	log := applog.New(&buf, "factory", "test")
	// 写一条信息级日志看信封。
	log.Info("factory http listening", "addr", ":8080")
	// 准备承接解出来的对象。
	var row map[string]any
	// 没能取出缓冲里已经写下的字节就停住本用例。
	if err := json.Unmarshal(buf.Bytes(), &row); err != nil {
		// 解不开这段结构就停住。
		t.Fatalf("json: %v raw=%s", err, buf.Bytes())
	}
	// 逐项处理，空的就不进入循环。
	for _, k := range []string{"ts", "level", "msg", "logger", "version", "addr"} {
		if _, ok := row[k]; !ok {
			// 缺了该有的内容就停住。
			t.Fatalf("missing %s in %s", k, buf.Bytes())
		}
	}
	// 日志字段和预期不符就进入失败。
	if row["msg"] != "factory http listening" || row["logger"] != "factory" {
		// 字段和预期不符就停住。
		t.Fatalf("fields %v", row)
	}
	// 日志字段和预期不符就进入失败。
	if row["level"] != "INFO" {
		// 级别不对就停住本用例。
		t.Fatalf("level %v", row["level"])
	}
}

// 信息级不该打出调试，信息级本身要留下。
func TestDebugHiddenAtInfo(t *testing.T) {
	// 钉住这一个环境值。
	t.Setenv("WMESH_LOG_LEVEL", "info")
	// 准备一块缓冲把输出接住，方便事后检查。
	var buf bytes.Buffer
	// 新建这一份后面要用的对象。
	log := applog.New(&buf, "factory", "test")
	// 写一条调试日志，信息级不该出现。
	log.Debug("noise")
	// 日志缓冲和预期不符就进入失败。
	if buf.Len() != 0 {
		// 调试日志不该出现，出现了就停住。
		t.Fatalf("debug leaked: %s", buf.Bytes())
	}
	// 按指定级别写一条日志。
	log.Log(nil, slog.LevelInfo, "ok")
	// 日志正文和预期不符就进入失败。
	if !bytes.Contains(buf.Bytes(), []byte(`"msg":"ok"`)) {
		// 结果和预期不符就停住。
		t.Fatalf("got %s", buf.Bytes())
	}
}
