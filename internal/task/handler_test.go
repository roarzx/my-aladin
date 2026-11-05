package task

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

type fakeCreator struct {
	gotCreator uint64
	gotWS      string
	gotReq     CreateRequest
	task       *domain.Task
	err        error
}

func (f *fakeCreator) Create(_ context.Context, creatorID uint64, workspaceID string, req CreateRequest) (*domain.Task, error) {
	f.gotCreator = creatorID
	f.gotWS = workspaceID
	f.gotReq = req
	return f.task, f.err
}

func newTestServer(svc Creator, withAuth bool) http.Handler {
	r := chi.NewRouter()
	if withAuth {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(auth.WithActor(req.Context(), auth.Actor{UserID: 5})))
			})
		})
	}
	r.Post("/workspaces/{wid}/issues", (&Handler{Service: svc}).Create)
	return r
}

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/workspaces/ws1/issues", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCreateHandlerSuccess(t *testing.T) {
	svc := &fakeCreator{task: &domain.Task{ID: "task-1", Status: domain.TaskPending}}
	rec := post(t, newTestServer(svc, true),
		`{"name":"n","mode":"ordinary","agentIds":["a1"],"plannerAgentId":"a1"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("状态 = %d, body = %s", rec.Code, rec.Body.String())
	}
	if svc.gotCreator != 5 || svc.gotWS != "ws1" {
		t.Fatalf("creator=%d ws=%q", svc.gotCreator, svc.gotWS)
	}
	if svc.gotReq.PlannerAgentID != "a1" || len(svc.gotReq.AgentIDs) != 1 {
		t.Fatalf("请求解析错误: %+v", svc.gotReq)
	}
	var body struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data["id"] != "task-1" || body.Data["status"] != "pending" {
		t.Fatalf("data = %+v", body.Data)
	}
}

func TestCreateHandlerRejectsBadJSON(t *testing.T) {
	rec := post(t, newTestServer(&fakeCreator{}, true), `{"name":`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态 = %d", rec.Code)
	}
	if code := handlerErrorCode(t, rec); code != errcode.ValidationFailed {
		t.Fatalf("错误码 = %s", code)
	}
}

func TestCreateHandlerRejectsUnknownField(t *testing.T) {
	rec := post(t, newTestServer(&fakeCreator{}, true),
		`{"name":"n","agentIds":["a"],"plannerAgentId":"a","typo":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态 = %d", rec.Code)
	}
}

func TestCreateHandlerPropagatesDomainError(t *testing.T) {
	svc := &fakeCreator{err: errcode.New(errcode.TemplateNotFound, "模板不存在")}
	rec := post(t, newTestServer(svc, true), `{"name":"n","agentIds":["a"],"plannerAgentId":"a"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态 = %d", rec.Code)
	}
	if code := handlerErrorCode(t, rec); code != errcode.TemplateNotFound {
		t.Fatalf("错误码 = %s", code)
	}
}

func TestCreateHandlerRequiresAuth(t *testing.T) {
	rec := post(t, newTestServer(&fakeCreator{}, false), `{"name":"n"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态 = %d", rec.Code)
	}
}

func handlerErrorCode(t *testing.T, rec *httptest.ResponseRecorder) errcode.Code {
	t.Helper()
	var body struct {
		Error struct {
			Code errcode.Code `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是统一错误体: %s", rec.Body.String())
	}
	return body.Error.Code
}
