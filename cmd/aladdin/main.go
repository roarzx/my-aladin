// Command aladdin 是运维 CLI：migrate、daemon 状态与日志、reset。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/roarzx/my-aladin/internal/buildinfo"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
)

const usage = `aladdin — 运维 CLI

用法:
  aladdin <command> [flags]

命令:
  version            打印版本
  migrate            执行数据库迁移（--dsn 或环境变量 MYSQL_DSN）
  daemon status      查看本机 daemon 状态（C 阶段实现）
  daemon logs        查看 daemon 日志（C 阶段实现）
  reset              清空本地运行状态（C 阶段实现，危险）
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "version":
		fmt.Println(buildinfo.String())
	case "migrate":
		err = runMigrate(os.Args[2:])
	case "daemon":
		err = errors.New("daemon 子命令在 C 阶段实现")
	case "reset":
		err = errors.New("reset 在 C 阶段实现")
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "aladdin:", err)
		os.Exit(1)
	}
}

func runMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	dsn := fs.String("dsn", os.Getenv("MYSQL_DSN"), "MySQL DSN（默认读 MYSQL_DSN）")
	down := fs.Int("down", 0, "回滚最近 N 个版本；0 表示向前迁移到最新")
	_ = fs.Parse(args)

	if *dsn == "" {
		return errors.New("migrate: 缺少 --dsn 或环境变量 MYSQL_DSN")
	}
	if *down < 0 {
		return errors.New("migrate: --down 不能为负数")
	}
	if *down > 0 {
		return mysqlstore.MigrateDown(*dsn, *down)
	}
	return mysqlstore.Migrate(*dsn)
}
