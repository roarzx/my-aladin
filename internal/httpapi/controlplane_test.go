package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// newControlPlaneRouter 挂载控制面路由。DB 传 nil：以下用例覆盖的校验分支
// 都在访问数据库之前返回。
func newControlPlaneRouter(db *sql.DB) http.Handler {
	members := fakeMembers{
		"ws1": {1: domain.RoleMember, 2: domain.RoleViewer},
	}
	return New(Deps{
		Authenticator: fakeAuth{},
		Members:       members,
		RegisterGlobal: func(r chi.Router) {
			(&ControlPlane{DB: db, Members: members}).Mount(r)
		},
	})
}

func doJSON(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, code errcode.Code) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("状态 = %d，期望 %d，响应 = %s", rec.Code, status, rec.Body.String())
	}
	if got := errorCode(t, rec); got != code {
		t.Fatalf("错误码 = %s，期望 %s", got, code)
	}
}

func TestCreateMachineRequiresWorkspaceID(t *testing.T) {
	rec := doJSON(t, newControlPlaneRouter(nil), http.MethodPost, "/machines", "member",
		`{"name":"m1"}`)
	assertError(t, rec, http.StatusBadRequest, errcode.ValidationFailed)
}

func TestCreateMachineRejectsEmptyName(t *testing.T) {
	rec := doJSON(t, newControlPlaneRouter(nil), http.MethodPost, "/machines", "member",
		`{"workspaceId":"ws1","name":"   "}`)
	assertError(t, rec, http.StatusBadRequest, errcode.ValidationFailed)
}

func TestCreateMachineRejectsLongName(t *testing.T) {
	body, err := json.Marshal(map[string]string{
		"workspaceId": "ws1",
		"name":        strings.Repeat("m", 129),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, newControlPlaneRouter(nil), http.MethodPost, "/machines", "member", string(body))
	assertError(t, rec, http.StatusBadRequest, errcode.ValidationFailed)
}

func TestCreateMachineRejectsBadKind(t *testing.T) {
	rec := doJSON(t, newControlPlaneRouter(nil), http.MethodPost, "/machines", "member",
		`{"workspaceId":"ws1","name":"m1","kind":"vm"}`)
	assertError(t, rec, http.StatusBadRequest, errcode.ValidationFailed)
}

func TestCreateMachineRejectsUnknownField(t *testing.T) {
	rec := doJSON(t, newControlPlaneRouter(nil), http.MethodPost, "/machines", "member",
		`{"workspaceId":"ws1","name":"m1","unexpected":1}`)
	assertError(t, rec, http.StatusBadRequest, errcode.ValidationFailed)
}

func TestCreateMachineRejectsViewer(t *testing.T) {
	rec := doJSON(t, newControlPlaneRouter(nil), http.MethodPost, "/machines", "viewer",
		`{"workspaceId":"ws1","name":"m1"}`)
	assertError(t, rec, http.StatusForbidden, errcode.Forbidden)
}

func TestCreateMachineRequiresAuth(t *testing.T) {
	rec := doJSON(t, newControlPlaneRouter(nil), http.MethodPost, "/machines", "",
		`{"workspaceId":"ws1","name":"m1"}`)
	assertError(t, rec, http.StatusUnauthorized, errcode.Unauthenticated)
}

func TestCreateMachineRejectsNonMember(t *testing.T) {
	rec := doJSON(t, newControlPlaneRouter(nil), http.MethodPost, "/machines", "member",
		`{"workspaceId":"ws2","name":"m1"}`)
	assertError(t, rec, http.StatusForbidden, errcode.Forbidden)
}

func TestListEndpointsRequireWorkspaceID(t *testing.T) {
	h := newControlPlaneRouter(nil)
	for _, path := range []string{"/machines", "/agents", "/repositories"} {
		t.Run(path, func(t *testing.T) {
			rec := do(t, h, http.MethodGet, path, "member")
			assertError(t, rec, http.StatusBadRequest, errcode.ValidationFailed)
		})
	}
}

func TestListEndpointsRejectNonMember(t *testing.T) {
	h := newControlPlaneRouter(nil)
	for _, path := range []string{"/machines", "/agents", "/repositories"} {
		t.Run(path, func(t *testing.T) {
			rec := do(t, h, http.MethodGet, path+"?workspaceId=ws2", "member")
			assertError(t, rec, http.StatusForbidden, errcode.Forbidden)
		})
	}
}

func TestCreateRepositoryValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"缺少地址", `{"workspaceId":"ws1"}`},
		{"地址含空格", `{"workspaceId":"ws1","url":"https://example.com/a b.git"}`},
		{"不支持的协议", `{"workspaceId":"ws1","url":"ftp://example.com/a.git"}`},
		{"provider 不合法", `{"workspaceId":"ws1","url":"https://example.com/a.git","provider":"gitlab"}`},
		{"未知字段", `{"workspaceId":"ws1","url":"https://example.com/a.git","extra":true}`},
	}
	h := newControlPlaneRouter(nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, h, http.MethodPost, "/repositories", "member", tc.body)
			assertError(t, rec, http.StatusBadRequest, errcode.ValidationFailed)
		})
	}
}

func TestCreateRepositoryRejectsViewer(t *testing.T) {
	rec := doJSON(t, newControlPlaneRouter(nil), http.MethodPost, "/repositories", "viewer",
		`{"workspaceId":"ws1","url":"https://example.com/a.git"}`)
	assertError(t, rec, http.StatusForbidden, errcode.Forbidden)
}

func TestCreateAgentValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"缺少名称", `{"workspaceId":"ws1","machineId":"m1","workDir":"/tmp/a"}`},
		{"名称过长", `{"workspaceId":"ws1","name":"` + strings.Repeat("a", 129) + `","machineId":"m1","workDir":"/tmp/a"}`},
		{"缺少机器", `{"workspaceId":"ws1","name":"a1","workDir":"/tmp/a"}`},
		{"workDir 非绝对路径", `{"workspaceId":"ws1","name":"a1","machineId":"m1","workDir":"tmp/a"}`},
		{"可见性不合法", `{"workspaceId":"ws1","name":"a1","machineId":"m1","workDir":"/tmp/a","visibility":"secret"}`},
		{"运行时类型不支持", `{"workspaceId":"ws1","name":"a1","machineId":"m1","workDir":"/tmp/a","runtimeType":"codex"}`},
		{"capabilitySet 非对象", `{"workspaceId":"ws1","name":"a1","machineId":"m1","workDir":"/tmp/a","capabilitySet":[1]}`},
		{"envVars 非对象", `{"workspaceId":"ws1","name":"a1","machineId":"m1","workDir":"/tmp/a","envVars":"x"}`},
	}
	h := newControlPlaneRouter(nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, h, http.MethodPost, "/agents", "member", tc.body)
			assertError(t, rec, http.StatusBadRequest, errcode.ValidationFailed)
		})
	}
}

func TestCreateAgentRejectsViewer(t *testing.T) {
	rec := doJSON(t, newControlPlaneRouter(nil), http.MethodPost, "/agents", "viewer",
		`{"workspaceId":"ws1","name":"a1","machineId":"m1","workDir":"/tmp/a"}`)
	assertError(t, rec, http.StatusForbidden, errcode.Forbidden)
}

func TestValidWorkDir(t *testing.T) {
	cases := map[string]bool{
		"/tmp/a":   true,
		" /tmp/a ": true,
		"/":        true,
		"":         false,
		"   ":      false,
		"tmp/a":    false,
		"./a":      false,
		"~/work/a": false,
	}
	for in, want := range cases {
		if got := validWorkDir(in); got != want {
			t.Errorf("validWorkDir(%q) = %v，期望 %v", in, got, want)
		}
	}
}

func TestNormalizeJSONObject(t *testing.T) {
	raw := func(s string) *json.RawMessage {
		v := json.RawMessage(s)
		return &v
	}
	cases := []struct {
		name    string
		in      *json.RawMessage
		want    string
		wantErr bool
	}{
		{"未提供", nil, "{}", false},
		{"空值", raw(""), "{}", false},
		{"显式 null", raw("null"), "{}", false},
		{"对象", raw(`{"a":1}`), `{"a":1}`, false},
		{"数组", raw(`[1]`), "", true},
		{"标量", raw(`123`), "", true},
		{"非法 JSON", raw(`{"a"`), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeJSONObject(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际返回 %s", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("未预期错误: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("结果 = %s，期望 %s", got, tc.want)
			}
		})
	}
}

func TestJSONOrEmptyObject(t *testing.T) {
	cases := []struct {
		in   json.RawMessage
		want string
	}{
		{nil, "{}"},
		{json.RawMessage("null"), "{}"},
		{json.RawMessage("  "), "{}"},
		{json.RawMessage(`{"a":1}`), `{"a":1}`},
	}
	for _, tc := range cases {
		if got := string(jsonOrEmptyObject(tc.in)); got != tc.want {
			t.Errorf("jsonOrEmptyObject(%s) = %s，期望 %s", tc.in, got, tc.want)
		}
	}
}
