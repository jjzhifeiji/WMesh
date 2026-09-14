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

var dbNameRe = regexp.MustCompile(`^wmesh_fac_[a-f0-9]{32}$`)

// DBName 用工厂稳定身份拼库名；重启后仍能按同一身份打开。
func DBName(factoryID uuid.UUID) string {
	return "wmesh_fac_" + stripDash(factoryID.String())
}

// stripDash 去掉 UUID 里的横线，用来拼库名。
func stripDash(s string) string {
	b := make([]byte, 0, 32)
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			b = append(b, s[i])
		}
	}
	return string(b)
}

// SwapDB 把维护库 DSN 换成目标厂库，不改账号密码。
func SwapDB(adminDSN, name string) (string, error) {
	if !dbNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid database name")
	}
	u, err := url.Parse(adminDSN)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

// Exists 看维护实例上是否已有该厂库。
func Exists(admin *gorm.DB, name string) (bool, error) {
	if !dbNameRe.MatchString(name) {
		return false, fmt.Errorf("invalid database name")
	}
	var exists bool
	err := admin.Raw("SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = ?)", name).Scan(&exists).Error
	return exists, err
}

// ListFactoryIDs 扫维护实例上已有的厂库身份，供本机列出已认领工厂。
func ListFactoryIDs(admin *gorm.DB) ([]uuid.UUID, error) {
	var names []string
	if err := admin.Raw("SELECT datname FROM pg_database WHERE datname LIKE 'wmesh_fac_%'").Scan(&names).Error; err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(names))
	for _, name := range names {
		id, ok := ParseDBName(name)
		if !ok {
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

// ParseDBName 从厂库名还原工厂稳定身份。
func ParseDBName(name string) (uuid.UUID, bool) {
	const prefix = "wmesh_fac_"
	if !strings.HasPrefix(name, prefix) || len(name) != len(prefix)+32 {
		return uuid.Nil, false
	}
	hex := name[len(prefix):]
	id, err := uuid.Parse(hex[0:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:32])
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// Ensure 若厂库不存在则新建；CREATE DATABASE 不能放在事务里。
func Ensure(admin *gorm.DB, name string) error {
	ok, err := Exists(admin, name)
	if err != nil || ok {
		return err
	}
	sqlDB, err := admin.DB()
	if err != nil {
		return err
	}
	_, err = sqlDB.Exec("CREATE DATABASE " + name)
	return err
}

// Drop 删掉该厂库；仅测试或回收用，业务路径不调用。
func Drop(admin *gorm.DB, name string) error {
	if !dbNameRe.MatchString(name) {
		return fmt.Errorf("invalid database name")
	}
	sqlDB, err := admin.DB()
	if err != nil {
		return err
	}
	_, err = sqlDB.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	return err
}

// OpenMigrated 打开厂库并套用尚未记录的向前迁移。
func OpenMigrated(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	if err := migrate.Up(db, migrations.FS, "."); err != nil {
		return nil, err
	}
	return db, nil
}
