// JSON 信封字段与级别开关。
package applog_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"wmesh/global/internal/platform/applog"
)

func TestJSONEnvelope(t *testing.T) {
	t.Setenv("WMESH_LOG_LEVEL", "info")
	var buf bytes.Buffer
	log := applog.New(&buf, "wmesh-global", "global", "test")
	log.Info("global http listening", "addr", ":8080")
	var row map[string]any
	if err := json.Unmarshal(buf.Bytes(), &row); err != nil {
		t.Fatalf("json: %v raw=%s", err, buf.Bytes())
	}
	for _, k := range []string{"@timestamp", "level", "message", "serviceName", "service_name", "version", "addr"} {
		if _, ok := row[k]; !ok {
			t.Fatalf("missing %s in %s", k, buf.Bytes())
		}
	}
	if row["message"] != "global http listening" || row["serviceName"] != "wmesh-global" || row["service_name"] != "global" {
		t.Fatalf("fields %v", row)
	}
	if row["level"] != "INFO" {
		t.Fatalf("level %v", row["level"])
	}
}

func TestDebugHiddenAtInfo(t *testing.T) {
	t.Setenv("WMESH_LOG_LEVEL", "info")
	var buf bytes.Buffer
	log := applog.New(&buf, "wmesh-global", "global", "test")
	log.Debug("noise")
	if buf.Len() != 0 {
		t.Fatalf("debug leaked: %s", buf.Bytes())
	}
	log.Log(nil, slog.LevelInfo, "ok")
	if !bytes.Contains(buf.Bytes(), []byte(`"message":"ok"`)) {
		t.Fatalf("got %s", buf.Bytes())
	}
}
