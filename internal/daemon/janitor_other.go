//go:build !unix

package daemon

import "io"

// Janitor 在非 unix 平台为空实现。
type Janitor struct{}

// StartJanitor 在非 unix 平台返回空 janitor。
func StartJanitor(string) (*Janitor, error) { return &Janitor{}, nil }

// Stop 空实现。
func (j *Janitor) Stop() {}

// RunJanitor 空实现。
func RunJanitor(string, io.Reader) error { return nil }
