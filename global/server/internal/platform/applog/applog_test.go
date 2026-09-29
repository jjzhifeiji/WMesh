// JSON 信封字段与级别开关。
package applog_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"wmesh/global/internal/platform/applog"
)

// 信息级应打出采集要的字段，级别写成大写。
func TestJSONEnvelope(t *testing.T) {
	// 固定信息级，避免环境把调试也放出来。
	t.Setenv("WMESH_LOG_LEVEL", "info")
	// 先攒在内存里，好拆这一行 JSON。
	var buf bytes.Buffer
	// 用测试缓冲代替标准输出。
	log := applog.New(&buf, "global", "test")
	// 打一条带地址的信息日志。
	log.Info("global http listening", "addr", ":8080")
	// 准备接拆开的字段。
	var row map[string]any
	// 拆不开就不是合法 JSON。
	if err := json.Unmarshal(buf.Bytes(), &row); err != nil {
		// 拆失败就中止并带上原文。
		t.Fatalf("json: %v raw=%s", err, buf.Bytes())
	}
	// 采集要的键必须都在。
	for _, k := range []string{"ts", "level", "msg", "logger", "version", "addr"} {
		// 缺一键这条信封就不完整。
		if _, ok := row[k]; !ok {
			// 缺键就中止并带上原文。
			t.Fatalf("missing %s in %s", k, buf.Bytes())
		}
	}
	// 正文和服务名必须是刚打的那条。
	if row["msg"] != "global http listening" || row["logger"] != "global" {
		// 对不上就中止并带上拆出的字段。
		t.Fatalf("fields %v", row)
	}
	// 信息级要写成大写，方便采集端过滤。
	if row["level"] != "INFO" {
		// 级别不对就中止并带上实际值。
		t.Fatalf("level %v", row["level"])
	}
}

// 信息级应藏住调试，信息级本身仍要打出。
func TestDebugHiddenAtInfo(t *testing.T) {
	// 固定信息级，调试不该出现。
	t.Setenv("WMESH_LOG_LEVEL", "info")
	// 先攒在内存里，看调试有没有漏出。
	var buf bytes.Buffer
	// 用测试缓冲代替标准输出。
	log := applog.New(&buf, "global", "test")
	// 调试在信息级应被丢掉。
	log.Debug("noise")
	// 缓冲里不该有调试那条。
	if buf.Len() != 0 {
		// 漏出来就中止并带上原文。
		t.Fatalf("debug leaked: %s", buf.Bytes())
	}
	// 信息级本身仍要打出。
	log.Log(nil, slog.LevelInfo, "ok")
	// 正文键必须在这一行里。
	if !bytes.Contains(buf.Bytes(), []byte(`"msg":"ok"`)) {
		// 没打出来就中止并带上原文。
		t.Fatalf("got %s", buf.Bytes())
	}
}
