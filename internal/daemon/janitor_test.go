//go:build unix

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain 让测试二进制同时充当 janitor 子进程（StartJanitor 以自身二进制启动）。
func TestMain(m *testing.M) {
	if dir := os.Getenv("ALADDIN_JANITOR_DIR"); dir != "" {
		pipe := os.NewFile(uintptr(3), "janitor-pipe")
		_ = RunJanitor(dir, pipe)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestJanitorKillsChildrenWhenDaemonDies 是端到端用例：
// helper 充当 daemon（启动 sleep + janitor），被 SIGKILL 后 janitor 应清理 sleep。
func TestJanitorKillsChildrenWhenDaemonDies(t *testing.T) {
	if os.Getenv("GO_WANT_DAEMON_HELPER") == "1" {
		dir := os.Getenv("HELPER_DIR")
		reaper, err := NewReaper(dir)
		if err != nil {
			os.Exit(2)
		}
		cmd := exec.Command("sleep", "60")
		if err := reaper.Start(cmd); err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(filepath.Join(dir, "child.pid"),
			[]byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
			os.Exit(4)
		}
		if _, err := StartJanitor(dir); err != nil {
			os.Exit(5)
		}
		for {
			time.Sleep(time.Hour)
		}
	}

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestJanitorKillsChildrenWhenDaemonDies")
	cmd.Env = append(os.Environ(), "GO_WANT_DAEMON_HELPER=1", "HELPER_DIR="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	pid := waitChildPID(t, filepath.Join(dir, "child.pid"), 5*time.Second)
	defer func() { _ = syscall.Kill(-pid, syscall.SIGKILL) }()
	if _, err := processIdentity(pid); err != nil {
		t.Fatal("子进程未启动")
	}

	// kill -9 daemon：管道被内核关闭，janitor 应清理子进程组。
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = cmd.Process.Wait()

	waitGone(t, pid, 5*time.Second)
}

func waitChildPID(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等待子进程 PID 超时")
	return 0
}
