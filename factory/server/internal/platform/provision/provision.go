// Package provision 按工厂稳定身份建/开厂库，不做业务判定，也不认 WAN 名录。
package provision

import (
	"fmt"
	"net/url"
	"regexp"

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

func stripDash(s string) string {
	b := make([]byte, 0, 32)
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			b = append(b, s[i])
		}
	}
	return string(b)
}

// SwapDB 把维护库 DSN 换成目标厂库，不改账号口令。
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
