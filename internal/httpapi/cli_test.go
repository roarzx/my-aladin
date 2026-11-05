package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/pkg/errcode"
)

type fakeContextStore struct {
	mu       sync.Mutex
	versions map[string]int
	data     map[string]map[int]string
	conflict bool
}

func newFakeContextStore() *fakeContextStore {
	return &fakeContextStore{versions: map[string]int{}, data: map[string]map[int]string{}}
}

func (f *fakeContextStore) ListVersions(context.Context, string, string, string) ([]ContextVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ContextVersion
	for kind, max := range f.versions {
		for v := 1; v <= max; v++ {
			out = append(out, ContextVersion{Kind: kind, Version: v})
		}
	}
	return out, nil
}

func (f *fakeContextStore) GetVersion(_ context.Context, _, _, _, kind string, version int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	versions := f.data[kind]
	if versions == nil {
		return "", sql.ErrNoRows
	}
	content, ok := versions[version]
	if !ok {
		return "", sql.ErrNoRows
	}
	return content, nil
}

func (f *fakeContextStore) PutVersion(_ context.Context, _, _, _, kind, content string, expected int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conflict {
		return 0, errcode.New(errcode.Conflict, "版本冲突")
	}
	if expected != f.versions[kind] {
		return 0, errcode.New(errcode.Conflict, "版本冲突")
	}
	version := f.versions[kind] + 1
	if f.data[kind] == nil {
		f.data[kind] = map[int]string{}
	}
	f.data[kind][version] = content
	f.versions[kind] = version
	return version, nil
}

func newCLITestServer(t *testing.T, store ContextStore) *httptest.Server {
	t.Helper()
	router := chi.NewRouter()
	router.Route("/api/v1/cli", func(r chi.Router) {
		r.Use(MachineAuth(func(req *http.Request) (string, error) {
			if req.Header.Get("Authorization") != "Bearer t" {
				return "", errors.New("bad token")
			}
			return "machine-1", nil
		}))
		MountCLI(r, store)
	})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

func cliDo(t *testing.T, srv *httptest.Server, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func TestCLIRequiresMachineAuth(t *testing.T) {
	srv := newCLITestServer(t, newFakeContextStore())
	status, body := cliDo(t, srv, http.MethodGet,
		"/api/v1/cli/contexts/t1/task/files?scopeId=t1", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("状态 = %d, body = %s", status, body)
	}
}

func TestCLIContextCRUD(t *testing.T) {
	srv := newCLITestServer(t, newFakeContextStore())
	base := "/api/v1/cli/contexts/t1/task"

	status, body := cliDo(t, srv, http.MethodPut, base+"/files/decisions", "t",
		map[string]any{"content": "v1", "expectedVersion": 0})
	if status != http.StatusOK {
		t.Fatalf("PUT 状态 = %d, body = %s", status, body)
	}

	status, body = cliDo(t, srv, http.MethodGet, base+"/files/decisions/1?scopeId=t1", "t", nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"content":"v1"`)) {
		t.Fatalf("GET 状态 = %d, body = %s", status, body)
	}

	status, body = cliDo(t, srv, http.MethodGet, base+"/files", "t", nil)
	if status != http.StatusOK || !bytes.Contains(body, []byte(`"version":1`)) {
		t.Fatalf("LIST 状态 = %d, body = %s", status, body)
	}

	status, _ = cliDo(t, srv, http.MethodGet, base+"/files/decisions/99?scopeId=t1", "t", nil)
	if status != http.StatusNotFound {
		t.Fatalf("缺失版本状态 = %d", status)
	}
}

func TestCLIConflict(t *testing.T) {
	store := newFakeContextStore()
	store.conflict = true
	srv := newCLITestServer(t, store)

	status, body := cliDo(t, srv, http.MethodPut,
		"/api/v1/cli/contexts/t1/task/files/decisions", "t",
		map[string]any{"content": "x", "expectedVersion": 0})
	if status != http.StatusConflict {
		t.Fatalf("状态 = %d, body = %s", status, body)
	}
}

func TestCLIRequiresScopeID(t *testing.T) {
	srv := newCLITestServer(t, newFakeContextStore())
	status, _ := cliDo(t, srv, http.MethodGet,
		"/api/v1/cli/contexts/t1/step/files", "t", nil)
	if status != http.StatusBadRequest {
		t.Fatalf("状态 = %d", status)
	}
}
