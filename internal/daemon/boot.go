// Package daemon 是执行面进程的装配入口。
//
// A 阶段只提供可运行的骨架：加载配置、校验必填项、等待退出信号。
// C1 会在此基础上加入 Profile 解析、控制面连接与命令分发。
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/roarzx/my-aladin/internal/buildinfo"
	"github.com/roarzx/my-aladin/internal/config"
)

// Options 是 daemon 的启动参数。
type Options struct {
	// Profile 对应 ~/.aladdin/profiles/<profile>/，同一机器可并行多个实例。
	Profile string
	// ConfigPath 是配置文件路径。
	ConfigPath string
}

// Run 启动 daemon 骨架，直到 ctx 取消。
func Run(ctx context.Context, opts Options) error {
	if opts.Profile == "" {
		return fmt.Errorf("profile: 不能为空")
	}
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	if cfg.Daemon.ServerURL == "" {
		return fmt.Errorf("daemon.server_url: 必填（或设置 ALADDIN_SERVER_URL）")
	}
	if os.Getenv("ALADDIN_MACHINE_TOKEN") == "" {
		slog.Warn("ALADDIN_MACHINE_TOKEN 未设置；C 阶段连接控制面时会失败",
			"profile", opts.Profile)
	}

	slog.Info("daemon 骨架启动",
		"version", buildinfo.String(),
		"profile", opts.Profile,
		"server", cfg.Daemon.ServerURL,
		"profile_dir", cfg.Daemon.ProfileDir,
	)

	<-ctx.Done()
	slog.Info("daemon 退出", "reason", ctx.Err())
	return nil
}
