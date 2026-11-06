// Package daemon 是执行面进程的装配入口。
//
// C-1 提供：Profile 解析、控制面连接、握手、心跳、重连与命令响应骨架。
// C-2 起在此基础上加入命令幂等、会话与 Provider 管理。
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/roarzx/my-aladin/internal/buildinfo"
	"github.com/roarzx/my-aladin/internal/config"
	"github.com/roarzx/my-aladin/internal/transport/acp"
)

// Options 是 daemon 的启动参数。
type Options struct {
	// Profile 对应 <profile_dir>/<profile>/，同一机器可并行多个实例。
	Profile string
	// ConfigPath 是配置文件路径。
	ConfigPath string
	// MachineID 为空时按 profile 目录中的 machine_id 解析或生成。
	MachineID string
}

// Run 启动 daemon，直到 ctx 取消或服务端拒绝连接。
func Run(ctx context.Context, opts Options) error {
	cfg, err := config.LoadFor(opts.ConfigPath, config.RoleDaemon)
	if err != nil {
		return err
	}

	profile, err := ResolveProfile(cfg.Daemon.ProfileDir, opts.Profile, opts.MachineID)
	if err != nil {
		return err
	}

	token := os.Getenv("ALADDIN_MACHINE_TOKEN")
	if token == "" {
		return errors.New("ALADDIN_MACHINE_TOKEN: 未设置（机器令牌见 9.5.1）")
	}

	idempotent, err := OpenIdempotentStore(filepath.Join(profile.Dir, "commands.jsonl"), 24*time.Hour)
	if err != nil {
		return err
	}
	defer func() { _ = idempotent.Close() }()

	sessionStore, err := OpenSessionStore(filepath.Join(profile.Dir, "session_history.json"))
	if err != nil {
		return err
	}

	reaper, err := NewReaper(profile.Dir)
	if err != nil {
		return err
	}
	janitor, err := StartJanitor(profile.Dir)
	if err != nil {
		return err
	}
	// LIFO：先 StopAll 子进程，再关闭 janitor 管道。
	defer janitor.Stop()
	defer func() { _ = reaper.StopAll() }()

	agentRuntime, ok := cfg.Agents["claude"]
	if !ok {
		return errors.New("agent.claude: 未配置")
	}
	running := NewRunningExecutions()
	providerConfig := acp.Config{
		Bin:                    agentRuntime.Bin,
		ExpectedVersion:        agentRuntime.ExpectedVersion,
		Cwd:                    profile.Dir,
		InitializeTimeout:      cfg.ACP.InitializeTimeout.Std(),
		InputTimeout:           time.Duration(agentRuntime.InputTimeoutSeconds) * time.Second,
		AutoApprovePermissions: agentRuntime.AutoApprove,
	}
	stepHandler := &StepHandler{
		ProviderConfig: providerConfig,
		Spawner:        reaper,
		Running:        running,
		Log:            slog.Default(),
	}
	if cfg.Daemon.APIURL != "" {
		cli := NewCLIClient(cfg.Daemon.APIURL, token)
		worker := &FlowNodeWorker{
			Client: cli, ProviderConfig: providerConfig, Spawner: reaper,
			Interval: 3 * time.Second, Heartbeat: 10 * time.Second, Log: slog.Default(),
		}
		go worker.Run(ctx)
	}
	dispatcher := NewDispatcher(idempotent, stepHandler.Handle, slog.Default())

	client := NewClient(ClientOptions{
		ACPURL:            cfg.Daemon.ACPURL,
		MachineID:         profile.MachineID,
		Token:             token,
		CLIVersion:        buildinfo.Version,
		HeartbeatInterval: cfg.Server.HeartbeatInterval.Std(),
		HeartbeatTimeout:  cfg.Server.HeartbeatTimeout.Std(),
		MaxMessageBytes:   cfg.ACP.MaxMessageBytes,
		Backoff:           Backoff{Base: 500 * time.Millisecond, Max: 30 * time.Second},
		OnCommand:         dispatcher.Handle,
		ActiveExecutions:  running.Snapshot,
	})

	slog.Info("daemon 启动",
		"version", buildinfo.String(),
		"profile", profile.Name,
		"profile_dir", profile.Dir,
		"machine", profile.MachineID,
		"acp_url", cfg.Daemon.ACPURL,
		"known_commands", idempotent.Len(),
		"known_sessions", sessionStore.Len(),
	)

	if err := client.Run(ctx); err != nil {
		return fmt.Errorf("daemon 连接: %w", err)
	}
	return nil
}
