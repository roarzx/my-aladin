// Package capability 提供 Agent 能力清单、执行前授权与审计（G7）。
package capability

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ErrUnknownAgent 表示没有该 Agent 的能力清单。
var ErrUnknownAgent = errors.New("capability: Agent 未知")

// ErrUnauthorized 表示工具未授权。
var ErrUnauthorized = errors.New("capability: 工具未授权")

// ErrUnavailable 表示能力不可用（不得静默降级）。
var ErrUnavailable = errors.New("capability: 能力不可用")

// Set 是 Agent 的能力清单。
type Set struct {
	Attachments bool     `json:"attachments"`
	Git         bool     `json:"git"`
	FlowNode    bool     `json:"flow_node"`
	Tools       []string `json:"tools"`
}

// Catalog 维护 agentID → Set，并记录授权审计。
type Catalog struct {
	mu    sync.RWMutex
	sets  map[string]Set
	audit func(agentID, tool string, allowed bool)
}

// NewCatalog 构造能力清单。
func NewCatalog() *Catalog {
	return &Catalog{sets: make(map[string]Set)}
}

// Put 登记 Agent 能力。
func (c *Catalog) Put(agentID string, set Set) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tools := append([]string(nil), set.Tools...)
	sort.Strings(tools)
	set.Tools = tools
	c.sets[agentID] = set
}

// SetAudit 设置审计回调（执行前授权结果写入日志/事件）。
func (c *Catalog) SetAudit(fn func(agentID, tool string, allowed bool)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.audit = fn
}

// Resolve 返回能力清单；未知 Agent 明确报错。
func (c *Catalog) Resolve(agentID string) (Set, error) {
	c.mu.RLock()
	set, ok := c.sets[agentID]
	c.mu.RUnlock()
	if !ok {
		return Set{}, fmt.Errorf("%w: %s", ErrUnknownAgent, agentID)
	}
	return set, nil
}

// Authorize 校验单个工具：能力位不可用时明确报错，未授权工具被拒绝并审计。
func (c *Catalog) Authorize(agentID, tool string) error {
	set, err := c.Resolve(agentID)
	if err != nil {
		c.record(agentID, tool, false)
		return err
	}
	if err := check(set, tool); err != nil {
		c.record(agentID, tool, false)
		return err
	}
	c.record(agentID, tool, true)
	return nil
}

// FilterTools 在派发前剔除未授权工具；能力不可用返回错误（不静默降级）。
func (c *Catalog) FilterTools(agentID string, requested []string) ([]string, []string, error) {
	set, err := c.Resolve(agentID)
	if err != nil {
		return nil, nil, err
	}
	allowed := make([]string, 0, len(requested))
	removed := make([]string, 0)
	for _, tool := range requested {
		capability := capabilityFor(tool)
		if capability != "" && !setHasCapability(set, capability) {
			return nil, nil, fmt.Errorf("%w: %s（工具 %s）", ErrUnavailable, capability, tool)
		}
		if !containsTool(set.Tools, tool) {
			removed = append(removed, tool)
			c.record(agentID, tool, false)
			continue
		}
		allowed = append(allowed, tool)
		c.record(agentID, tool, true)
	}
	return allowed, removed, nil
}

func check(set Set, tool string) error {
	if capability := capabilityFor(tool); capability != "" && !setHasCapability(set, capability) {
		return fmt.Errorf("%w: %s", ErrUnavailable, capability)
	}
	if !containsTool(set.Tools, tool) {
		return fmt.Errorf("%w: %s", ErrUnauthorized, tool)
	}
	return nil
}

func capabilityFor(tool string) string {
	switch {
	case tool == "attachments":
		return "attachments"
	case strings.HasPrefix(tool, "git"):
		return "git"
	case strings.HasPrefix(tool, "flow_node"):
		return "flow_node"
	default:
		return ""
	}
}

func setHasCapability(set Set, capability string) bool {
	switch capability {
	case "attachments":
		return set.Attachments
	case "git":
		return set.Git
	case "flow_node":
		return set.FlowNode
	default:
		return false
	}
}

func containsTool(tools []string, tool string) bool {
	for _, candidate := range tools {
		if candidate == tool {
			return true
		}
	}
	return false
}

func (c *Catalog) record(agentID, tool string, allowed bool) {
	c.mu.RLock()
	fn := c.audit
	c.mu.RUnlock()
	if fn != nil {
		fn(agentID, tool, allowed)
	}
}
