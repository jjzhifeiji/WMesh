// Package provision 按工厂稳定身份建/开厂库，不做业务判定，也不认 WAN 名录。
package provision

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/migrate"
	"wmesh/factory/migrations"
)

// 厂库名形态，固定前缀加三十二位十六进制。
var dbNameRe = regexp.MustCompile(`^wmesh_fac_[a-f0-9]{32}$`)

// DBName 用工厂稳定身份拼库名；重启后仍能按同一身份打开。
func DBName(factoryID uuid.UUID) string {
	// 交回收成普通文本再拿去比较或拼接的结果。
	return "wmesh_fac_" + stripDash(factoryID.String())
}

// stripDash 去掉 UUID 里的横线，用来拼库名。
func stripDash(s string) string {
	// 按需要的长度把缓冲准备好。
	b := make([]byte, 0, 32)
	// 逐个字符看过去，横线不要放进库名。
	for i := 0; i < len(s); i++ {
		// 这个字符不是横线才放进库名。
		if s[i] != '-' {
			// 这个字符不是横线，放进库名。
			b = append(b, s[i])
		}
	}
	// 去掉横线后把库名用的文本交回去。
	return string(b)
}

// SwapDB 把维护库 DSN 换成目标厂库，不改账号密码。
func SwapDB(adminDSN, name string) (string, error) {
	// 库名形态不对就拒绝，避免连错库。
	if !dbNameRe.MatchString(name) {
		// 带上原因交回去，调用方才能知道为何停下。
		return "", fmt.Errorf("invalid database name")
	}
	// 把连接串拆成可替换的各段。
	u, err := url.Parse(adminDSN)
	// 没能把连接串拆成可替换的各段就停，避免带着残缺继续。
	if err != nil {
		return "", err
	}
	// 只换连接串里的库名，账号和参数不动。
	u.Path = "/" + name
	// 交回收成普通文本再拿去比较或拼接的结果。
	return u.String(), nil
}

// Exists 看维护实例上是否已有该厂库。
func Exists(admin *gorm.DB, name string) (bool, error) {
	// 库名形态不对就拒绝，避免连错库。
	if !dbNameRe.MatchString(name) {
		// 库名形态不对，拒绝连上去。
		return false, fmt.Errorf("invalid database name")
	}
	// 准备放下是否已有。
	var exists bool
	// 把查询结果扫进变量。
	err := admin.Raw("SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = ?)", name).Scan(&exists).Error
	return exists, err
}

// ListFactoryIDs 扫维护实例上已有的厂库身份，供本机列出已认领工厂。
func ListFactoryIDs(admin *gorm.DB) ([]uuid.UUID, error) {
	// 准备收集字符串结果。
	var names []string
	// 没能把查询结果扫进变量就停，避免带着残缺继续。
	if err := admin.Raw("SELECT datname FROM pg_database WHERE datname LIKE 'wmesh_fac_%'").Scan(&names).Error; err != nil {
		return nil, err
	}
	// 按需要的长度把缓冲准备好。
	out := make([]uuid.UUID, 0, len(names))
	// 按文件名逐个处理，已经做过的跳过。
	for _, name := range names {
		// 从库名里解析出工厂身份。
		id, ok := ParseDBName(name)
		// 没有命中就走另一路，不用零值冒充有值。
		if !ok {
			continue
		}
		// 把这一段接进结果，顺序要保持住。
		out = append(out, id)
	}
	return out, nil
}

// ParseDBName 从厂库名还原工厂稳定身份。
func ParseDBName(name string) (uuid.UUID, bool) {
	// 定下厂库名前缀，后面用来拼接和核对。
	const prefix = "wmesh_fac_"
	if !strings.HasPrefix(name, prefix) || len(name) != len(prefix)+32 {
		return uuid.Nil, false
	}
	// 看有多长，空的和超限的要分开处理。
	hex := name[len(prefix):]
	// 把文本收成稳定身份。
	id, err := uuid.Parse(hex[0:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:32])
	// 没能把文本收成稳定身份就停，避免带着残缺继续。
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// Ensure 若厂库不存在则新建；CREATE DATABASE 不能放在事务里。
func Ensure(admin *gorm.DB, name string) error {
	// 看这个厂库是不是已经建过。
	ok, err := Exists(admin, name)
	// 出错或条件不够就停下，避免半对的结果往下用。
	if err != nil || ok {
		return err
	}
	// 取出底层连接好去建库或删库。
	sqlDB, err := admin.DB()
	// 没能取出底层连接好去建库或删库就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 把这一句交给库去执行。
	_, err = sqlDB.Exec("CREATE DATABASE " + name)
	return err
}

// Drop 删掉该厂库；仅测试或回收用，业务路径不调用。
func Drop(admin *gorm.DB, name string) error {
	// 库名形态不对就拒绝，避免连错库。
	if !dbNameRe.MatchString(name) {
		// 库名形态不对，拒绝连上去。
		return fmt.Errorf("invalid database name")
	}
	// 取出底层连接好去建库或删库。
	sqlDB, err := admin.DB()
	// 没能取出底层连接好去建库或删库就停，避免带着残缺继续。
	if err != nil {
		return err
	}
	// 把这一句交给库去执行。
	_, err = sqlDB.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	return err
}

// OpenMigrated 打开厂库并套用尚未记录的向前迁移。
func OpenMigrated(dsn string) (*gorm.DB, error) {
	// 把库日志关掉，免得刷屏。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	// 没能把库日志关掉，免得刷屏就停，避免带着残缺继续。
	if err != nil {
		return nil, err
	}
	// 没能把库结构迁到当前版本就停，避免带着残缺继续。
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		return nil, err
	}
	return db, nil
}
