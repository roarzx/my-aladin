package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/roarzx/my-aladin/internal/daemon"
)

func runDaemon(args []string) error {
	if len(args) == 0 || args[0] != "start" {
		return errors.New("用法: aladdin daemon start --machine-id <id> --server-auth-token <token>")
	}
	fs := flag.NewFlagSet("daemon start", flag.ExitOnError)
	machineID := fs.String("machine-id", "", "机器 ID")
	token := fs.String("server-auth-token", "", "机器令牌（也可用 ALADDIN_MACHINE_TOKEN）")
	configPath := fs.String("config", "", "配置文件；为空时使用内置默认值")
	profile := fs.String("profile", "default", "本地 profile 名称")
	profileDir := fs.String("profile-dir", "", "profile 根目录（默认 ~/.aladdin/profiles）")
	acpURL := fs.String("acp-url", "", "控制面 WS 地址（默认 ws://127.0.0.1:8080/v1/acp）")
	apiURL := fs.String("api-url", "", "控制面 HTTP 地址（默认 http://127.0.0.1:8080）")
	_ = fs.Parse(args[1:])

	if *machineID == "" {
		return errors.New("daemon start: --machine-id 必填")
	}
	opts := daemon.Options{
		Profile: *profile, ConfigPath: *configPath, MachineID: *machineID,
		Token: *token, ACPURL: *acpURL, APIURL: *apiURL, ProfileDir: *profileDir,
	}
	if opts.ConfigPath == "" {
		// 不依赖配置文件；--acp-url/--api-url 可覆盖内置默认值
		opts.Config = daemon.DefaultConfig()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := daemon.Run(ctx, opts)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
