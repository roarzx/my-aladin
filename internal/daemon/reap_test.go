//go:build unix

package daemon

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func waitGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := processIdentity(pid); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("进程 %d 在 %v 内未退出", pid, timeout)
}

// waitExit 等待子进程被回收（被杀后需要 Wait 才能从僵尸态消失）。
func waitExit(t *testing.T, cmd *exec.Cmd, timeout time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("子进程未被终止")
	}
}

func TestStopAllKillsProcessGroup(t *testing.T) {
	reaper, err := NewReaper(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "60")
	if err := reaper.Start(cmd); err != nil {
		t.Fatalf("启动: %v", err)
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }()

	if _, err := processIdentity(cmd.Process.Pid); err != nil {
		t.Fatal("子进程未启动")
	}
	if err := reaper.StopAll(); err != nil {
		t.Fatal(err)
	}
	waitExit(t, cmd, 3*time.Second)
	waitGone(t, cmd.Process.Pid, 3*time.Second)
}

func TestCleanupStaleSkipsReusedPID(t *testing.T) {
	dir := t.TempDir()
	reaper, err := NewReaper(dir)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "60")
	if err := reaper.Start(cmd); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }()

	// 模拟 PID 已被复用：登记里的身份与当前进程不一致。
	path := filepath.Join(dir, childrenFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var registry childRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatal(err)
	}
	for i := range registry.Children {
		registry.Children[i].Identity = "bogus-identity"
	}
	data, _ = json.Marshal(registry)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := CleanupStale(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := processIdentity(cmd.Process.Pid); err != nil {
		t.Fatal("身份不匹配时不应误杀进程")
	}

	if err := reaper.StopAll(); err != nil {
		t.Fatal(err)
	}
	waitExit(t, cmd, 3*time.Second)
	waitGone(t, cmd.Process.Pid, 3*time.Second)
}

func TestWaitRemovesRegistryEntry(t *testing.T) {
	dir := t.TempDir()
	reaper, err := NewReaper(dir)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("true")
	if err := reaper.Start(cmd); err != nil {
		t.Fatal(err)
	}
	if err := reaper.Wait(cmd); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, childrenFileName))
	if err != nil {
		t.Fatal(err)
	}
	var registry childRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatal(err)
	}
	if len(registry.Children) != 0 {
		t.Fatalf("登记未清理: %+v", registry.Children)
	}
}
