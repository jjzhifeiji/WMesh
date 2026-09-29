// Package applog 把服务端日志收成一行 JSON，给 Alloy 采到 Loki。
// 服务身份在容器标签 obs.product / obs.service 上，不写进日志正文。
package applog

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New 写 JSON 到 w（空则 stdout），并设为默认 logger。
// loggerName 是打出日志的组件，version 是构建版本。
func New(w io.Writer, loggerName, version string) *slog.Logger {
	// 没给输出就写到标准输出，给容器采集。
	if w == nil {
		w = os.Stdout
	}
	// 按环境级别把日志收成 JSON。
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       levelFromEnv(),
		ReplaceAttr: jsonAttr,
	})
	// 组件名和版本留在正文，不作为 Loki 标签。
	log := slog.New(h).With(
		"logger", loggerName,
		"version", version,
	)
	// 设成进程默认，别的包打日志也进同一份。
	slog.SetDefault(log)
	return log
}

// jsonAttr 把时间和正文收成规范里的 ts / msg。
func jsonAttr(groups []string, a slog.Attr) slog.Attr {
	// 分组里的字段保持原样，避免改乱嵌套。
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.TimeKey:
		// 收成 UTC 毫秒时间，Alloy 用它做 Loki 时间。
		if a.Value.Kind() == slog.KindTime {
			return slog.String("ts", a.Value.Time().UTC().Format("2006-01-02T15:04:05.000Z"))
		}
	case slog.MessageKey:
		return slog.String("msg", a.Value.String())
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
