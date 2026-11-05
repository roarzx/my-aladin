package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

type fakeMembers map[string]map[uint64]domain.Role

func (f fakeMembers) MemberRole(_ context.Context, ws string, uid uint64) (domain.Role, error) {
	if m, ok := f[ws]; ok {
		if role, ok := m[uid]; ok {
			return role, nil
		}
	}
	return "", sql.ErrNoRows
}

type fakeAuth struct{}

func (fakeAuth) Authenticate(r *http.Request) (auth.Actor, error) {
	switch r.Header.Get("Authorization") {
	case "Bearer member":
		return auth.Actor{UserID: 1}, nil
	case "Bearer viewer":
		return auth.Actor{UserID: 2}, nil
	}
	return auth.Actor{}, errcode.New(errcode.Unauthenticated, "令牌无效")
}

func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func testRouter() http.Handler {
	return New(Deps{
		Authenticator: fakeAuth{},
		Members: fakeMembers{
			"ws1": {1: domain.RoleMember, 2: domain.RoleViewer},
		},
		Health: okHandler,
		Ready:  okHandler,
		RegisterGlobal: func(r chi.Router) {
			r.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
		},
		RegisterWorkspace: func(r chi.Router) {
			r.With(auth.RequireAction(auth.ActionCreateIssue, nil)).
				Post("/issues", func(w http.ResponseWriter, _ *http.Request) {
					Data(w, http.StatusCreated, map[string]string{"id": "issue-1"})
				})
		},
	})
}

func do(t *testing.T, h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) errcode.Code {
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

func TestHealthBypassesAuth(t *testing.T) {
	rec := do(t, testRouter(), http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 = %d", rec.Code)
	}
}

func TestUnknownRouteUnifiedError(t *testing.T) {
	rec := do(t, testRouter(), http.MethodGet, "/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态 = %d", rec.Code)
	}
	if code := errorCode(t, rec); code != errcode.NotFound {
		t.Fatalf("错误码 = %s", code)
	}
}

func TestProtectedRouteRequiresAuth(t *testing.T) {
	rec := do(t, testRouter(), http.MethodPost, "/workspaces/ws1/issues", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态 = %d", rec.Code)
	}
	if code := errorCode(t, rec); code != errcode.Unauthenticated {
		t.Fatalf("错误码 = %s", code)
	}
}

func TestViewerForbidden(t *testing.T) {
	rec := do(t, testRouter(), http.MethodPost, "/workspaces/ws1/issues", "viewer")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("状态 = %d", rec.Code)
	}
	if code := errorCode(t, rec); code != errcode.Forbidden {
		t.Fatalf("错误码 = %s", code)
	}
}

func TestMemberAllowed(t *testing.T) {
	rec := do(t, testRouter(), http.MethodPost, "/workspaces/ws1/issues", "member")
	if rec.Code != http.StatusCreated {
		t.Fatalf("状态 = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data["id"] != "issue-1" {
		t.Fatalf("data = %+v", body.Data)
	}
}

func TestNonMemberForbidden(t *testing.T) {
	rec := do(t, testRouter(), http.MethodPost, "/workspaces/ws2/issues", "member")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("状态 = %d", rec.Code)
	}
}

func TestPanicRecoveredAs500(t *testing.T) {
	rec := do(t, testRouter(), http.MethodGet, "/panic", "member")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态 = %d", rec.Code)
	}
	if code := errorCode(t, rec); code != errcode.InternalError {
		t.Fatalf("错误码 = %s", code)
	}
}

func TestRequestIDEchoed(t *testing.T) {
	h := testRouter()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "req-abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); got != "req-abc" {
		t.Fatalf("X-Request-ID = %q", got)
	}
	if rec.Header().Get("X-Trace-ID") == "" {
		t.Fatal("应生成 X-Trace-ID")
	}
}
