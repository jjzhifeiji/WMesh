package domain

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation 把数据库唯一冲突收成业务可判断的错误。
func IsUniqueViolation(err error) bool {
	// 准备承接数据库错误，好对照错误码。
	var pg *pgconn.PgError
	// 交回收成具体那一种错误的结果。
	return errors.As(err, &pg) && pg.Code == "23505"
}

// IsCheckViolation 把检查约束失败收成业务可判断的错误。
func IsCheckViolation(err error) bool {
	// 准备承接数据库错误，好对照错误码。
	var pg *pgconn.PgError
	// 交回收成具体那一种错误的结果。
	return errors.As(err, &pg) && pg.Code == "23514"
}

// IsForeignKeyViolation 把外键不存在收成业务可判断的错误。
func IsForeignKeyViolation(err error) bool {
	// 准备承接数据库错误，好对照错误码。
	var pg *pgconn.PgError
	// 交回收成具体那一种错误的结果。
	return errors.As(err, &pg) && pg.Code == "23503"
}
