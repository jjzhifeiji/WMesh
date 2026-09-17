// JSON 信封字段与级别开关。
package applog_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"wmesh/factory/internal/platform/applog"
)

func TestJSONEnvelope(t *testing.T) {
	t.Setenv("WMESH_LOG_LEVEL", "info")
	var buf bytes.Buffer
	log := applog.New(&buf, "wmesh-factory", "factory", "test")
	log.Info("factory http listening", "addr", ":8080")
	var row map[string]any
	if err := json.Unmarshal(buf.Bytes(), &row); err != nil {
		t.Fatalf("json: %v raw=%s", err, buf.Bytes())
	}
	for _, k := range []string{"@timestamp", "level", "message", "serviceName", "service_name", "version", "addr"} {
		if _, ok := row[k]; !ok {
			t.Fatalf("missing %s in %s", k, buf.Bytes())
		}
	}
	if row["message"] != "factory http listening" || row["serviceName"] != "wmesh-factory" || row["service_name"] != "factory" {
		t.Fatalf("fields %v", row)
	}
	if row["level"] != "INFO" {
		t.Fatalf("level %v", row["level"])
	}
}

func TestDebugHiddenAtInfo(t *testing.T) {
	t.Setenv("WMESH_LOG_LEVEL", "info")
	var buf bytes.Buffer
	log := applog.New(&buf, "wmesh-factory", "factory", "test")
	log.Debug("noise")
	if buf.Len() != 0 {
		t.Fatalf("debug leaked: %s", buf.Bytes())
	}
	log.Log(nil, slog.LevelInfo, "ok")
	if !bytes.Contains(buf.Bytes(), []byte(`"message":"ok"`)) {
		t.Fatalf("got %s", buf.Bytes())
	}
}
