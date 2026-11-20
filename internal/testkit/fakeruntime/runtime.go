package fakeruntime

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/roarzx/my-aladin/internal/runtime"
)

// ErrDispatch 模拟派发阶段断连。
var ErrDispatch = errors.New("fakeruntime: dispatch failed")

// Runtime 是 Runtime 接口的可编排实现。
type Runtime struct {
	mu         sync.Mutex
	script     *Script
	byID       map[string]*ScriptStep
	next       int
	dispatched []runtime.DispatchRequest
	cancelled  map[string]bool
	caps       runtime.CapabilitySet

	// OnEvent 在回放事件时被同步调用，便于测试断言。
	OnEvent func(Event)
}

// New 构造一个按脚本回放的假 Runtime。
func New(script *Script) *Runtime {
	r := &Runtime{
		script:    script,
		byID:      make(map[string]*ScriptStep),
		cancelled: make(map[string]bool),
	}
	if script != nil {
		for i := range script.Steps {
			step := &script.Steps[i]
			if step.ExecutionID != "" {
				r.byID[step.ExecutionID] = step
			}
		}
	}
	return r
}

// SetCapabilities 设置上报的能力。
func (r *Runtime) SetCapabilities(c runtime.CapabilitySet) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.caps = c
}

// Capabilities 实现 Runtime。
func (r *Runtime) Capabilities() runtime.CapabilitySet {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.caps
}

// Dispatch 按脚本回放事件；事件在返回前同步送达（便于确定性测试）。
func (r *Runtime) Dispatch(_ context.Context, req runtime.DispatchRequest) (string, error) {
	r.mu.Lock()
	r.dispatched = append(r.dispatched, req)
	step := r.pickLocked(req.ExecutionID)
	callback := r.OnEvent
	r.mu.Unlock()
	if step == nil {
		return "", errors.New("fakeruntime: 没有匹配的脚本")
	}
	if step.FailDispatch {
		return "", ErrDispatch
	}
	if !step.Hang {
		for _, ev := range step.Events {
			delay(ev.DelayMS)
			if callback != nil {
				callback(ev)
			}
		}
	}
	return req.ExecutionID, nil
}

// Cancel 把执行标记为已取消。
func (r *Runtime) Cancel(_ context.Context, executionID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[executionID]; !ok && r.script != nil && len(r.script.Steps) == 0 {
		return false, fmt.Errorf("fakeruntime: 未知执行 %s", executionID)
	}
	r.cancelled[executionID] = true
	return true, nil
}

// Dispatched 返回历次派发请求（测试断言用）。
func (r *Runtime) Dispatched() []runtime.DispatchRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]runtime.DispatchRequest, len(r.dispatched))
	copy(out, r.dispatched)
	return out
}

func (r *Runtime) pickLocked(id string) *ScriptStep {
	if step, ok := r.byID[id]; ok {
		return step
	}
	if r.script == nil || r.next >= len(r.script.Steps) {
		return nil
	}
	step := &r.script.Steps[r.next]
	r.next++
	return step
}
