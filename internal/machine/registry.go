// Package machine 定义机器能力与注册模型（B10）。
package machine

import (
	"encoding/json"
	"fmt"
	"time"
)

// Kind 对应 machines.kind。
type Kind string

const (
	KindLocal Kind = "local"
	KindCloud Kind = "cloud"
)

// Status 对应 machines.status。
type Status string

const (
	StatusOnline   Status = "online"
	StatusOffline  Status = "offline"
	StatusDisabled Status = "disabled"
)

// Capability 是机器声明的一项能力。
type Capability string

const (
	CapAttachments Capability = "attachments"
	CapGit         Capability = "git"
	CapFlowNode    Capability = "flow_node"
)

// Machine 是执行机器记录。
type Machine struct {
	ID           string
	WorkspaceID  string
	Name         string
	Kind         Kind
	Status       Status
	Capabilities []Capability
	CLIVersion   string
	LastSeenAt   *time.Time
}

// Supports 判断机器是否声明了某项能力。
func (m Machine) Supports(c Capability) bool {
	for _, got := range m.Capabilities {
		if got == c {
			return true
		}
	}
	return false
}

// ParseCapabilities 解析 capabilities JSON（字符串数组）。
func ParseCapabilities(raw json.RawMessage) ([]Capability, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, fmt.Errorf("解析机器能力: %w", err)
	}
	out := make([]Capability, 0, len(names))
	for _, n := range names {
		out = append(out, Capability(n))
	}
	return out, nil
}

// EncodeCapabilities 序列化能力列表，缺省为 `[]`。
func EncodeCapabilities(caps []Capability) (json.RawMessage, error) {
	if caps == nil {
		caps = []Capability{}
	}
	names := make([]string, len(caps))
	for i, c := range caps {
		names[i] = string(c)
	}
	return json.Marshal(names)
}
