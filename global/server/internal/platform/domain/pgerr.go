package domain

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation 把数据库唯一冲突收成业务可判断的错误。
func IsUniqueViolation(err error) bool {
	// 接住驱动错误，后面看是不是唯一冲突。
	var pg *pgconn.PgError
	// 转成驱动错误后只认唯一冲突。
	return errors.As(err, &pg) && pg.Code == "23505"
}

// UniqueConstraint 取出冲突的约束名，供 store 区分哪条唯一键。
func UniqueConstraint(err error) string {
	// 接住驱动错误，准备取出约束名。
	var pg *pgconn.PgError
	// 对上驱动错误才有约束名可取。
	if errors.As(err, &pg) {
		return pg.ConstraintName
	}
	return ""
}

// IsCheckViolation 把检查约束失败收成业务可判断的错误。
func IsCheckViolation(err error) bool {
	// 接住驱动错误，后面看检查约束。
	var pg *pgconn.PgError
	// 转成驱动错误后只认检查约束失败。
	return errors.As(err, &pg) && pg.Code == "23514"
}

// IsForeignKeyViolation 把外键不存在收成业务可判断的错误。
func IsForeignKeyViolation(err error) bool {
	// 接住驱动错误，后面看外键。
	var pg *pgconn.PgError
	// 转成驱动错误后只认外键不存在。
	return errors.As(err, &pg) && pg.Code == "23503"
}
