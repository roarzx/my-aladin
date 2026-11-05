//go:build unix

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const childrenFileName = "children.json"

type childEntry struct {
	PID       int       `json:"pid"`
	PGID      int       `json:"pgid"`
	Identity  string    `json:"identity"`
	Command   string    `json:"command"`
	StartedAt time.Time `json:"started_at"`
}

type childRegistry struct {
	Children []childEntry `json:"children"`
}

// Reaper 在独立进程组中托管 Provider 子进程（C12）。
type Reaper struct {
	dir     string
	mu      sync.Mutex
	entries []childEntry
}

// NewReaper 创建回收器；子进程登记写入 <dir>/children.json。
func NewReaper(dir string) (*Reaper, error) {
	if dir == "" {
		return nil, errors.New("reaper: 目录不能为空")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 reaper 目录: %w", err)
	}
	return &Reaper{dir: dir}, nil
}

// Start 在独立进程组中启动命令并登记 PID + 进程身份。
func (r *Reaper) Start(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	if err := cmd.Start(); err != nil {
		return err
	}
	entry := childEntry{
		PID:       cmd.Process.Pid,
		PGID:      cmd.Process.Pid,
		Command:   strings.Join(cmd.Args, " "),
		StartedAt: time.Now().UTC(),
	}
	if identity, err := processIdentity(cmd.Process.Pid); err == nil {
		entry.Identity = identity
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
	return r.saveLocked()
}

// Wait 等待子进程退出并移除登记。
func (r *Reaper) Wait(cmd *exec.Cmd) error {
	err := cmd.Wait()
	r.remove(cmd.Process.Pid)
	return err
}

// StopAll 终止当前登记的全部进程组（正常退出路径）。
func (r *Reaper) StopAll() error {
	r.mu.Lock()
	entries := append([]childEntry(nil), r.entries...)
	r.entries = nil
	err := r.saveLocked()
	r.mu.Unlock()

	for _, entry := range entries {
		killGroup(entry)
	}
	return err
}

func (r *Reaper) remove(pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.entries[:0]
	for _, entry := range r.entries {
		if entry.PID != pid {
			out = append(out, entry)
		}
	}
	r.entries = out
	_ = r.saveLocked()
}

func (r *Reaper) saveLocked() error {
	data, err := json.MarshalIndent(childRegistry{Children: r.entries}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(r.dir, childrenFileName), append(data, '\n'), 0o600)
}

// CleanupStale 由 janitor 调用：只杀身份匹配（或原进程已消失）的进程组，
// 避免 PID 复用导致误杀。
func CleanupStale(dir string) error {
	path := filepath.Join(dir, childrenFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取子进程登记: %w", err)
	}

	var registry childRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		_ = os.Remove(path)
		return nil
	}
	for _, entry := range registry.Children {
		killGroup(entry)
	}
	_ = os.Remove(path)
	return nil
}

func killGroup(entry childEntry) {
	if entry.PGID <= 0 {
		return
	}
	// 身份不匹配说明 PID 已被复用，跳过；ps 失败说明原进程已消失，仍可安全清理其进程组。
	if current, err := processIdentity(entry.PID); err == nil && entry.Identity != "" && current != entry.Identity {
		return
	}
	_ = syscall.Kill(-entry.PGID, syscall.SIGKILL)
}

// processIdentity 用启动时间标识进程，避免仅凭 PID 判断。
func processIdentity(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	identity := strings.TrimSpace(string(out))
	if identity == "" {
		return "", errors.New("进程不存在")
	}
	return identity, nil
}
