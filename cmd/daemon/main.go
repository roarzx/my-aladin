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
	var opts daemon.Options
	flag.StringVar(&opts.Profile, "profile", "default", "profile 名称（对应 ~/.aladdin/profiles/<profile>/）")
	flag.StringVar(&opts.ConfigPath, "config", "config/config.yaml", "配置文件路径")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := daemon.Run(ctx, opts); err != nil {
		fmt.Fprintln(os.Stderr, "daemon:", err)
		os.Exit(1)
	}
}
