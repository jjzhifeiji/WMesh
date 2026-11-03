// Package factorytz 给出本厂切日用的时区：按日报表和上送 WAN 的日期键都按它算，两边才对得上。
package factorytz

import (
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // 内嵌时区库，镜像没装 tzdata 也能按名字加载
)

const (
	EnvKey  = "WMESH_FACTORY_TZ" // 本厂时区的环境变量，填 IANA 名
	Default = "Asia/Shanghai"    // 没配或配错时的本厂时区
)

// 首次取用时读一次环境变量，之后整个进程同一个时区。
var (
	once sync.Once      // 只加载一次
	loc  *time.Location // 加载好的本厂时区
)

// Location 交回本厂时区；没配按 Asia/Shanghai，配错记告警后同样退回默认。
func Location() *time.Location {
	// 只在第一次读环境变量。
	once.Do(func() {
		// 按环境变量加载，出错退回默认。
		loc = Load(os.Getenv(EnvKey))
	})
	return loc
}

// Load 按 IANA 名加载时区；空按默认，不认识的名字或 Local 记告警后退回默认。
func Load(raw string) *time.Location {
	// 去掉空白，空的按默认。
	name := strings.TrimSpace(raw)
	// 没配就用默认时区。
	if name == "" {
		// 改用默认时区名。
		name = Default
	}
	// Local 随宿主机变，库里切日认不出，不收。
	if name != "Local" {
		// 按名字加载，内嵌时区库保证默认一定能加载。
		if l, err := time.LoadLocation(name); err == nil {
			return l
		}
	}
	// 记下配错的名字，运维能看见。
	slog.Warn("invalid factory time zone, fallback", "env", EnvKey, "value", raw, "fallback", Default)
	// 默认时区由内嵌库保证能加载。
	l, _ := time.LoadLocation(Default)
	return l
}
