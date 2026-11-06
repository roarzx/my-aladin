package mysql

import (
	"errors"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// isDuplicateKey 判断是否为唯一键冲突（MySQL 1062）。
func isDuplicateKey(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
