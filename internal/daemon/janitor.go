//go:build unix

package daemon

import (
	"io"
	"os"
	"os/exec"
	"syscall"
)

// RunJanitor 阻塞直到 daemon 退出（管道 EOF，SIGKILL 也会关闭 fd），然后清理遗留子进程。
func RunJanitor(dir string, pipe io.Reader) error {
	_, _ = io.Copy(io.Discard, pipe)
	return CleanupStale(dir)
}

// Janitor 是独立的守护进程，保证 daemon 被 SIGKILL 时子进程仍会被回收。
type Janitor struct {
	cmd  *exec.Cmd
	pipe *os.File
}

// StartJanitor 以自身二进制启动一个 detached janitor，并把管道写端留在 daemon 手中。
func StartJanitor(dir string) (*Janitor, error) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return nil, err
	}

	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "ALADDIN_JANITOR_DIR="+dir)
	cmd.ExtraFiles = []*os.File{readEnd} // 子进程侧为 fd 3
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = readEnd.Close()
		_ = writeEnd.Close()
		return nil, err
	}
	_ = readEnd.Close()
	return &Janitor{cmd: cmd, pipe: writeEnd}, nil
}

// Stop 在正常退出时关闭管道并等待 janitor 完成清理。
func (j *Janitor) Stop() {
	if j == nil {
		return
	}
	_ = j.pipe.Close()
	_ = j.cmd.Wait()
}
