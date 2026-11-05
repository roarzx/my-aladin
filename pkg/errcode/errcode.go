// Package errcode 定义对外错误码与 HTTP 映射（dev_spec 9.6）。
//
// 错误码表是闭合的：接口只能返回这里定义的码。新增码必须同步更新 9.6。
package errcode

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Code 是机器可识别的错误码。
type Code string

const (
	ValidationFailed       Code = "VALIDATION_FAILED"
	Unauthenticated        Code = "UNAUTHENTICATED"
	Forbidden              Code = "FORBIDDEN"
	NotFound               Code = "NOT_FOUND"
	Conflict               Code = "CONFLICT"
	AgentUnavailable       Code = "AGENT_UNAVAILABLE"
	MachineOffline         Code = "MACHINE_OFFLINE"
	RuntimeStartFailed     Code = "RUNTIME_START_FAILED"
	ResultUnknown          Code = "RESULT_UNKNOWN"
	UpstreamUnavailable    Code = "UPSTREAM_UNAVAILABLE"
	RevisionConflict       Code = "REVISION_CONFLICT"
	HashMismatch           Code = "HASH_MISMATCH"
	PlanSuperseded         Code = "PLAN_SUPERSEDED"
	NotConfirmed           Code = "NOT_CONFIRMED"
	AgentRefInvalid        Code = "AGENT_REF_INVALID"
	PlannerNotInMembers    Code = "PLANNER_NOT_IN_MEMBERS"
	StepNotFailed          Code = "STEP_NOT_FAILED"
	RetryBudgetExceeded    Code = "RETRY_BUDGET_EXCEEDED"
	AlreadyTerminal        Code = "ALREADY_TERMINAL"
	TaskNotChattable       Code = "TASK_NOT_CHATTABLE"
	VerificationIncomplete Code = "VERIFICATION_INCOMPLETE"
	TemplateNotFound       Code = "TEMPLATE_NOT_FOUND"
	RunTerminal            Code = "RUN_TERMINAL"
	IdempotencyKeyReused   Code = "IDEMPOTENCY_KEY_REUSED"
	StepNotReconciling     Code = "STEP_NOT_RECONCILING"
	InternalError          Code = "INTERNAL_ERROR"
)

var statuses = map[Code]int{
	ValidationFailed:       http.StatusBadRequest,
	Unauthenticated:        http.StatusUnauthorized,
	Forbidden:              http.StatusForbidden,
	NotFound:               http.StatusNotFound,
	TemplateNotFound:       http.StatusNotFound,
	Conflict:               http.StatusConflict,
	RevisionConflict:       http.StatusConflict,
	HashMismatch:           http.StatusConflict,
	PlanSuperseded:         http.StatusConflict,
	NotConfirmed:           http.StatusConflict,
	StepNotFailed:          http.StatusConflict,
	AlreadyTerminal:        http.StatusConflict,
	TaskNotChattable:       http.StatusConflict,
	RunTerminal:            http.StatusConflict,
	IdempotencyKeyReused:   http.StatusConflict,
	StepNotReconciling:     http.StatusConflict,
	AgentUnavailable:       http.StatusUnprocessableEntity,
	MachineOffline:         http.StatusUnprocessableEntity,
	RuntimeStartFailed:     http.StatusUnprocessableEntity,
	AgentRefInvalid:        http.StatusUnprocessableEntity,
	PlannerNotInMembers:    http.StatusUnprocessableEntity,
	RetryBudgetExceeded:    http.StatusUnprocessableEntity,
	VerificationIncomplete: http.StatusUnprocessableEntity,
	InternalError:          http.StatusInternalServerError,
	ResultUnknown:          http.StatusServiceUnavailable,
	UpstreamUnavailable:    http.StatusServiceUnavailable,
}

// Status 返回错误码对应的 HTTP 状态；未知码视为 500。
func (c Code) Status() int {
	if s, ok := statuses[c]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// Valid 表示错误码在闭合表内。
func (c Code) Valid() bool {
	_, ok := statuses[c]
	return ok
}

// Error 是带错误码的错误。
type Error struct {
	Code    Code
	Message string
	Details any
}

// Error 实现 error。
func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// New 构造一个带错误码的错误。
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Newf 构造一个格式化消息的错误。
func Newf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WithDetails 附加机器可读细节。
func (e *Error) WithDetails(details any) *Error {
	e.Details = details
	return e
}

// As 从错误链中取出 *Error。
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

type errorPayload struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type errorBody struct {
	Error errorPayload `json:"error"`
}

// Write 按统一错误体写响应；未知错误对客户端统一显示为 INTERNAL_ERROR。
func Write(w http.ResponseWriter, err error) {
	e, ok := As(err)
	if !ok {
		e = New(InternalError, "internal error")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Code.Status())
	_ = json.NewEncoder(w).Encode(errorBody{Error: errorPayload{
		Code:    e.Code,
		Message: e.Message,
		Details: e.Details,
	}})
}
