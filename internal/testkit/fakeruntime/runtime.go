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
	mu     sync.Mutex
	script *Script
	byID   map[string]*ScriptStep
	next   int
	states map[string]runtime.ExecutionSnapshot
	caps   runtime.CapabilitySet

	// OnEvent 在回放事件时被同步调用，便于测试断言。
	OnEvent func(Event)
}

// New 构造一个按脚本回放的假 Runtime。
func New(script *Script) *Runtime {
	r := &Runtime{
		script: script,
		byID:   make(map[string]*ScriptStep),
		states: make(map[string]runtime.ExecutionSnapshot),
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

// Dispatch 按脚本执行；事件回放与状态落定在返回前完成（便于确定性测试）。
func (r *Runtime) Dispatch(_ context.Context, req runtime.DispatchRequest) (string, error) {
	r.mu.Lock()
	step := r.pickLocked(req.ExecutionID)
	r.mu.Unlock()
	if step == nil {
		return "", errors.New("fakeruntime: 没有匹配的脚本")
	}
	if step.FailDispatch {
		return "", ErrDispatch
	}

	id := req.ExecutionID
	if id == "" {
		id = step.ExecutionID
		if id == "" {
			id = fmt.Sprintf("fake-exec-%d", r.next)
		}
	}

	r.mu.Lock()
	r.states[id] = runtime.ExecutionSnapshot{ExecutionID: id, State: runtime.StateRunning}
	callback := r.OnEvent
	r.mu.Unlock()

	// 模拟"超时不响应"：保持 running，等待 Cancel 或由调用方超时处理。
	if step.Hang {
		return id, nil
	}

	for _, ev := range step.Events {
		delay(ev.DelayMS)
		if callback != nil {
			callback(ev)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.states[id].State == runtime.StateCancelled {
		return id, nil
	}
	if step.Result == "failed" {
		r.states[id] = runtime.ExecutionSnapshot{
			ExecutionID: id,
			State:       runtime.StateFailed,
			ErrorClass:  step.ErrorClass,
		}
		return id, nil
	}
	r.states[id] = runtime.ExecutionSnapshot{ExecutionID: id, State: runtime.StateCompleted}
	return id, nil
}

// Inspect 返回执行快照。
func (r *Runtime) Inspect(_ context.Context, executionID string) (runtime.ExecutionSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap, ok := r.states[executionID]
	if !ok {
		return runtime.ExecutionSnapshot{}, fmt.Errorf("fakeruntime: 未知执行 %s", executionID)
	}
	return snap, nil
}

// Cancel 将 running 的执行置为 cancelled。
func (r *Runtime) Cancel(_ context.Context, executionID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap, ok := r.states[executionID]
	if !ok {
		return false, fmt.Errorf("fakeruntime: 未知执行 %s", executionID)
	}
	if snap.State != runtime.StateRunning && snap.State != runtime.StateDispatched {
		return false, nil
	}
	r.states[executionID] = runtime.ExecutionSnapshot{ExecutionID: executionID, State: runtime.StateCancelled}
	return true, nil
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
