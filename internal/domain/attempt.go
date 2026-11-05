package domain

import (
	"fmt"
	"time"
)

// AttemptStatus 对应 attempts.status。
type AttemptStatus string

const (
	AttemptDispatched AttemptStatus = "dispatched"
	AttemptRunning    AttemptStatus = "running"
	AttemptCompleted  AttemptStatus = "completed"
	AttemptFailed     AttemptStatus = "failed"
	AttemptUnknown    AttemptStatus = "unknown"
)

var allAttemptStatuses = []AttemptStatus{
	AttemptDispatched, AttemptRunning, AttemptCompleted, AttemptFailed, AttemptUnknown,
}

// Valid 校验状态取值。
func (s AttemptStatus) Valid() error {
	for _, v := range allAttemptStatuses {
		if s == v {
			return nil
		}
	}
	return fmt.Errorf("未知的 Attempt 状态 %q", s)
}

// IsTerminal 表示尝试已落定。unknown 仍需对账，不算终态。
func (s AttemptStatus) IsTerminal() bool {
	return s == AttemptCompleted || s == AttemptFailed
}

// ErrorClass 是失败分类（dev_spec 3.4），决定重试策略。
type ErrorClass string

const (
	ErrNetwork      ErrorClass = "network"
	ErrRuntime      ErrorClass = "runtime"
	ErrTool         ErrorClass = "tool"
	ErrVerification ErrorClass = "verification"
	ErrPermission   ErrorClass = "permission"
)

var allErrorClasses = []ErrorClass{
	ErrNetwork, ErrRuntime, ErrTool, ErrVerification, ErrPermission,
}

// Valid 校验分类取值。
func (c ErrorClass) Valid() error {
	for _, v := range allErrorClasses {
		if c == v {
			return nil
		}
	}
	return fmt.Errorf("未知的错误分类 %q", c)
}

// Retryable 表示该类失败可以按 3.4 的策略自动重试。
func (c ErrorClass) Retryable() bool {
	return c == ErrNetwork || c == ErrRuntime || c == ErrTool
}

// Attempt 是一次执行尝试；业务重试会新建 Attempt，不覆盖失败证据。
type Attempt struct {
	ID           string
	StepID       string
	AttemptNo    uint32
	ExecutionID  string
	LeaseEpoch   uint64
	Status       AttemptStatus
	ErrorClass   ErrorClass
	ResultDigest string
	CommitSHA    string
	CreatedAt    time.Time
}
