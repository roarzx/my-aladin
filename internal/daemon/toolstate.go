package daemon

import (
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// ToolCall 是一次工具调用的配对状态（C8）。
type ToolCall struct {
	ID        string
	Title     string
	Kind      string
	Status    string
	StartedAt time.Time
	UpdatedAt time.Time
	Result    json.RawMessage
}

// ToolState 维护 tool_call / tool_call_update 的配对，不依赖消息历史。
type ToolState struct {
	mu     sync.Mutex
	open   map[string]*ToolCall
	opened int
	closed int
}

// NewToolState 构造工具调用状态机。
func NewToolState() *ToolState {
	return &ToolState{open: make(map[string]*ToolCall)}
}

// StartToolCall 记录一次工具调用；重复 id 视为同一次调用。
func (s *ToolState) StartToolCall(id, title, kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.open[id]; !exists {
		s.opened++
	}
	now := time.Now().UTC()
	s.open[id] = &ToolCall{
		ID: id, Title: title, Kind: kind,
		Status: "pending", StartedAt: now, UpdatedAt: now,
	}
}

// UpdateToolCall 更新调用状态；终态时移出未闭合集合。
func (s *ToolState) UpdateToolCall(id, status string, result json.RawMessage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	call, ok := s.open[id]
	if !ok {
		return false
	}
	call.Status = status
	call.UpdatedAt = time.Now().UTC()
	if len(result) > 0 {
		call.Result = append(json.RawMessage(nil), result...)
	}
	if isTerminalToolStatus(status) {
		delete(s.open, id)
		s.closed++
	}
	return true
}

// Open 返回未闭合的工具调用（按开始时间排序）。
func (s *ToolState) Open() []ToolCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ToolCall, 0, len(s.open))
	for _, call := range s.open {
		out = append(out, *call)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

// Stats 返回累计开始与闭合数量。
func (s *ToolState) Stats() (opened, closed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opened, s.closed
}

func isTerminalToolStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	}
	return false
}
