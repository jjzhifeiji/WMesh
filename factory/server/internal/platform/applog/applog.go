// Package applog 把服务端日志收成一份 JSON，给 Alloy 采到 Loki；不管业务字段。
package applog

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// New 写 JSON 到 w（空则 stdout），带上服务名后设为默认 logger。
func New(w io.Writer, serviceName, serviceShort, version string) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       levelFromEnv(),
		ReplaceAttr: jsonAttr,
	})
	log := slog.New(h).With(
		"serviceName", serviceName,
		"service_name", serviceShort,
		"version", version,
	)
	slog.SetDefault(log)
	return log
}

// jsonAttr 把时间、正文改成 Loki 里已有的 @timestamp / message。
func jsonAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.TimeKey:
		if a.Value.Kind() == slog.KindTime {
			return slog.String("@timestamp", a.Value.Time().UTC().Format(time.RFC3339Nano))
		}
	case slog.MessageKey:
		return slog.String("message", a.Value.String())
	}
	return a
}

// levelFromEnv 读 WMESH_LOG_LEVEL；认不出就用 info。
func levelFromEnv() slog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WMESH_LOG_LEVEL"))) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
