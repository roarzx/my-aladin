package daemon

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/httpapi"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

type memContextStore struct {
	mu   sync.Mutex
	data map[string]map[int]string // kind -> version -> content
}

func newMemContextStore() *memContextStore {
	return &memContextStore{data: map[string]map[int]string{}}
}

func (m *memContextStore) ListVersions(context.Context, string, string, string) ([]httpapi.ContextVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []httpapi.ContextVersion
	for kind, versions := range m.data {
		for version := range versions {
			out = append(out, httpapi.ContextVersion{Kind: kind, Version: version})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Version < out[j].Version
	})
	return out, nil
}

func (m *memContextStore) GetVersion(_ context.Context, _, _, _, kind string, version int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	content, ok := m.data[kind][version]
	if !ok {
		return "", sql.ErrNoRows
	}
	return content, nil
}

func (m *memContextStore) PutVersion(_ context.Context, _, _, _, kind, content string, expected int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	versions := m.data[kind]
	current := 0
	for version := range versions {
		if version > current {
			current = version
		}
	}
	if expected != current {
		return 0, errcode.New(errcode.Conflict, "版本冲突")
	}
	version := current + 1
	if versions == nil {
		versions = map[int]string{}
		m.data[kind] = versions
	}
	versions[version] = content
	return version, nil
}

func newContextTestServer(t *testing.T, store httpapi.ContextStore) *httptest.Server {
	t.Helper()
	router := chi.NewRouter()
	router.Route("/api/v1/cli", func(r chi.Router) {
		r.Use(httpapi.MachineAuth(func(*http.Request) (string, error) { return "machine-1", nil }))
		httpapi.MountCLI(r, store)
	})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

func TestContextSyncPushPullAndMerge(t *testing.T) {
	store := newMemContextStore()
	srv := newContextTestServer(t, store)
	ctx := context.Background()

	meta := t.TempDir()
	sync, err := OpenContextSync(meta, NewCLIClient(srv.URL, "t"))
	if err != nil {
		t.Fatal(err)
	}

	// 空仓库：Pull 返回版本 0
	if content, version, err := sync.Pull(ctx, "task1", "task", "task1", "decisions"); err != nil || version != 0 || content != "" {
		t.Fatalf("Pull 空: %q %d %v", content, version, err)
	}

	// 首次 Push：expected 0 → 1
	version, err := sync.Push(ctx, "task1", "task", "task1", "decisions", "local-1", nil)
	if err != nil || version != 1 {
		t.Fatalf("Push: %d, err=%v", version, err)
	}
	if got := sync.LocalVersion("decisions", "task1"); got != 1 {
		t.Fatalf("本地版本 = %d", got)
	}

	// 外部写者推进到 2；Pull 后本地跟上
	other := NewCLIClient(srv.URL, "t")
	if v, err := other.PutContext(ctx, "task1", "task", "task1", "decisions", "remote-1", 1); err != nil || v != 2 {
		t.Fatalf("外部写入: %d, err=%v", v, err)
	}
	content, version, err := sync.Pull(ctx, "task1", "task", "task1", "decisions")
	if err != nil || content != "remote-1" || version != 2 {
		t.Fatalf("Pull: %q %d %v", content, version, err)
	}
	if got := sync.LocalVersion("decisions", "task1"); got != 2 {
		t.Fatalf("Pull 后本地版本 = %d", got)
	}

	// 外部再推到 3，本地落后；带 merge 的 Push 应合并后成功
	if v, err := other.PutContext(ctx, "task1", "task", "task1", "decisions", "remote-2", 2); err != nil || v != 3 {
		t.Fatalf("外部写入 2: %d, err=%v", v, err)
	}
	var mergedFrom string
	version, err = sync.Push(ctx, "task1", "task", "task1", "decisions", "local-2",
		func(remote string) (string, error) {
			mergedFrom = remote
			return remote + "+local-2", nil
		})
	if err != nil || version != 4 {
		t.Fatalf("冲突合并: %d, err=%v", version, err)
	}
	if mergedFrom != "remote-2" {
		t.Fatalf("merge 收到 %q", mergedFrom)
	}
	latest, latestVersion, err := other.GetLatest(ctx, "task1", "task", "task1", "decisions")
	if err != nil || latestVersion != 4 || latest != "remote-2+local-2" {
		t.Fatalf("最终内容 = %q v%d err=%v", latest, latestVersion, err)
	}

	data, err := os.ReadFile(filepath.Join(meta, ".context_versions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"decisions/task1": 4`) {
		t.Fatalf("版本文件 = %s", data)
	}
}

func TestContextSyncConflictWithoutMerge(t *testing.T) {
	store := newMemContextStore()
	srv := newContextTestServer(t, store)
	ctx := context.Background()

	sync, err := OpenContextSync(t.TempDir(), NewCLIClient(srv.URL, "t"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sync.Push(ctx, "t1", "task", "t1", "decisions", "v1", nil); err != nil {
		t.Fatal(err)
	}

	// 外部推进后，本地不拉取直接推 → 冲突
	other := NewCLIClient(srv.URL, "t")
	if _, err := other.PutContext(ctx, "t1", "task", "t1", "decisions", "remote", 1); err != nil {
		t.Fatal(err)
	}
	_, err = sync.Push(ctx, "t1", "task", "t1", "decisions", "v2", nil)
	if !errors.Is(err, ErrContextConflict) {
		t.Fatalf("期望 ErrContextConflict，实际: %v", err)
	}
}
