// Command daemon 是执行面入口，每台执行机器运行一个。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/roarzx/my-aladin/internal/daemon"
)

func main() {
	// 作为 janitor 子进程启动（由 StartJanitor 通过环境变量触发）。
	if dir := os.Getenv("ALADDIN_JANITOR_DIR"); dir != "" {
		pipe := os.NewFile(uintptr(3), "janitor-pipe")
		if err := daemon.RunJanitor(dir, pipe); err != nil {
			fmt.Fprintln(os.Stderr, "janitor:", err)
			os.Exit(1)
		}
		return
	}

	var opts daemon.Options
	flag.StringVar(&opts.Profile, "profile", "default", "profile 名称（对应 <profile_dir>/<profile>/）")
	flag.StringVar(&opts.ConfigPath, "config", "config/config.yaml", "配置文件路径")
	flag.StringVar(&opts.MachineID, "machine-id", "", "机器 ID（默认从 profile 目录读取或生成）")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := daemon.Run(ctx, opts); err != nil {
		fmt.Fprintln(os.Stderr, "daemon:", err)
		os.Exit(1)
	}
}
