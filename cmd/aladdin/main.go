// Command aladdin 是运维 CLI：migrate、daemon 状态与日志、reset。
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/roarzx/my-aladin/internal/buildinfo"
	"github.com/roarzx/my-aladin/internal/machine"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
)

const usage = `aladdin — 运维 CLI

用法:
  aladdin <command> [flags]

命令:
  version            打印版本
  migrate            执行数据库迁移（--dsn 或环境变量 MYSQL_DSN）
  machine token      为机器签发一次性令牌（明文只打印一次）
  squad action       推进工作项（start|complete|block|cancel）
  squad handoff      交接工作项负责人
  squad context      读取 Agent 上下文
  squad artifact     按字节区间读取方案内容
  daemon start       启动本机 daemon（--machine-id / --server-auth-token）
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
	case "machine":
		err = runMachine(os.Args[2:])
	case "squad":
		err = runSquad(os.Args[2:])
	case "daemon":
		err = runDaemon(os.Args[2:])
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

func runMachine(args []string) error {
	if len(args) == 0 || args[0] != "token" {
		return errors.New("用法: aladdin machine token --machine-id <id> [--ttl 24h] [--dsn ...]")
	}
	fs := flag.NewFlagSet("machine token", flag.ExitOnError)
	dsn := fs.String("dsn", os.Getenv("MYSQL_DSN"), "MySQL DSN（默认读 MYSQL_DSN）")
	machineID := fs.String("machine-id", "", "机器 ID")
	ttl := fs.Duration("ttl", 24*time.Hour, "令牌有效期")
	_ = fs.Parse(args[1:])

	if *dsn == "" || *machineID == "" {
		return errors.New("machine token: 需要 --machine-id 与 --dsn（或 MYSQL_DSN）")
	}
	if *ttl <= 0 {
		return errors.New("machine token: --ttl 必须大于 0")
	}

	db, err := sql.Open("mysql", *dsn)
	if err != nil {
		return fmt.Errorf("打开 MySQL: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("连接 MySQL: %w", err)
	}

	token, err := machine.NewToken()
	if err != nil {
		return err
	}
	expiresAt := time.Now().UTC().Add(*ttl)
	if err := mysqlstore.CreateMachineToken(context.Background(), db, *machineID, machine.HashToken(token), expiresAt); err != nil {
		return err
	}
	fmt.Println(token)
	return nil
}
