package errcode

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAllCodesHaveStatus(t *testing.T) {
	codes := []Code{
		ValidationFailed, Unauthenticated, Forbidden, NotFound, Conflict,
		AgentUnavailable, MachineOffline, RuntimeStartFailed, ResultUnknown,
		UpstreamUnavailable, RevisionConflict, HashMismatch, PlanSuperseded,
		NotConfirmed, AgentRefInvalid, PlannerNotInMembers, StepNotFailed,
		RetryBudgetExceeded, AlreadyTerminal, TaskNotChattable,
		VerificationIncomplete, TemplateNotFound, RunTerminal,
		IdempotencyKeyReused, StepNotReconciling, InternalError,
	}
	for _, c := range codes {
		if !c.Valid() {
			t.Errorf("%q 缺少 HTTP 状态映射", c)
		}
		if c.Status() < 400 || c.Status() > 599 {
			t.Errorf("%q.Status() = %d", c, c.Status())
		}
	}
	if Code("NOPE").Valid() {
		t.Error("未知码不应合法")
	}
	if got := Code("NOPE").Status(); got != http.StatusInternalServerError {
		t.Errorf("未知码状态 = %d", got)
	}
}

func TestWriteErrorBody(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, New(Forbidden, "no access").WithDetails(map[string]string{"scope": "ws"}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("状态 = %d", rec.Code)
	}
	var body struct {
		Error struct {
			Code    Code              `json:"code"`
			Message string            `json:"message"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if body.Error.Code != Forbidden || body.Error.Message != "no access" {
		t.Fatalf("响应体 = %+v", body.Error)
	}
	if body.Error.Details["scope"] != "ws" {
		t.Fatalf("details 丢失: %+v", body.Error.Details)
	}
}

func TestWriteUnknownErrorDoesNotLeak(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, errors.New("sql: connection refused password=hunter2"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态 = %d", rec.Code)
	}
	if got := rec.Body.String(); got == "" || strings.Contains(got, "hunter2") {
		t.Fatalf("未知错误不应泄露内部信息: %s", got)
	}
}
