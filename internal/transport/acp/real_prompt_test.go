//go:build realacp

package acp

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestRealPromptEditsFile 用真实 claude-agent-acp 修改一个临时仓库文件，
// 验证权限自动批准与文件编辑链路。会消耗 Claude 额度，默认不进入 CI。
func TestRealPromptEditsFile(t *testing.T) {
	bin := os.Getenv("ALADDIN_ACP_BIN")
	if bin == "" {
		t.Skip("未设置 ALADDIN_ACP_BIN")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "aladdin@example.com")
	run("config", "user.name", "Aladdin Bot")
	if err := os.WriteFile(dir+"/README.md", []byte("# demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	p := NewProvider(Config{
		Bin: bin, ExpectedVersion: os.Getenv("ALADDIN_ACP_VERSION"), Cwd: dir,
		SystemPrompt:           "你是一个只修改当前仓库文件的编码 Agent。",
		AutoApprovePermissions: true,
		InitializeTimeout:      20 * time.Second, InputTimeout: 180 * time.Second,
	}, DefaultSpawner{}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	session, err := p.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.Prompt(ctx,
		"在当前仓库的 README.md 末尾追加一行：hello from aladdin。只改这个文件，完成后结束。", nil)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if result.StopReason != "end_turn" {
		t.Fatalf("stopReason = %q", result.StopReason)
	}
	data, err := os.ReadFile(dir + "/README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello from aladdin") {
		t.Fatalf("README 未被修改:\n%s", data)
	}
}
