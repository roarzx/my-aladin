//go:build !unix

package daemon

import (
	"errors"
	"os/exec"
)

// Reaper 在非 unix 平台不可用（v1 只支持 Linux / macOS）。
type Reaper struct{}

// NewReaper 返回一个不可用的 Reaper。
func NewReaper(string) (*Reaper, error) { return &Reaper{}, nil }

// Start 在非 unix 平台返回错误。
func (r *Reaper) Start(*exec.Cmd) error {
	return errors.New("进程回收仅支持 unix 平台")
}

// Wait 直接等待进程。
func (r *Reaper) Wait(cmd *exec.Cmd) error { return cmd.Wait() }

// StopAll 空实现。
func (r *Reaper) StopAll() error { return nil }

// CleanupStale 空实现。
func CleanupStale(string) error { return nil }
