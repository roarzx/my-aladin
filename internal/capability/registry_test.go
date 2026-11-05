package capability_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/roarzx/my-aladin/internal/capability"
)

type auditRecord struct {
	tool    string
	allowed bool
}

func TestAuthorize(t *testing.T) {
	catalog := capability.NewCatalog()
	catalog.Put("agent-1", capability.Set{
		Git:   true,
		Tools: []string{"search", "git_diff"},
	})
	var mu sync.Mutex
	var audits []auditRecord
	catalog.SetAudit(func(_ string, tool string, allowed bool) {
		mu.Lock()
		defer mu.Unlock()
		audits = append(audits, auditRecord{tool: tool, allowed: allowed})
	})

	if err := catalog.Authorize("agent-1", "search"); err != nil {
		t.Fatalf("search 应授权: %v", err)
	}
	if err := catalog.Authorize("agent-1", "shell"); !errors.Is(err, capability.ErrUnauthorized) {
		t.Fatalf("未授权工具应拒绝，实际: %v", err)
	}
	if err := catalog.Authorize("agent-1", "attachments"); !errors.Is(err, capability.ErrUnavailable) {
		t.Fatalf("能力不可用应明确报错，实际: %v", err)
	}
	if err := catalog.Authorize("unknown", "search"); !errors.Is(err, capability.ErrUnknownAgent) {
		t.Fatalf("未知 Agent 应报错，实际: %v", err)
	}

	allowed, removed, err := catalog.FilterTools("agent-1", []string{"search", "shell", "git_diff"})
	if err != nil {
		t.Fatal(err)
	}
	if len(allowed) != 2 || allowed[0] != "search" || allowed[1] != "git_diff" {
		t.Fatalf("allowed = %+v", allowed)
	}
	if len(removed) != 1 || removed[0] != "shell" {
		t.Fatalf("removed = %+v", removed)
	}

	// 能力位不可用时不得静默剔除
	if _, _, err := catalog.FilterTools("agent-1", []string{"search", "flow_node"}); !errors.Is(err, capability.ErrUnavailable) {
		t.Fatalf("flow_node 应报不可用，实际: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(audits) == 0 {
		t.Fatal("应记录授权审计")
	}
	foundDenied := false
	for _, record := range audits {
		if record.tool == "shell" && !record.allowed {
			foundDenied = true
		}
	}
	if !foundDenied {
		t.Fatalf("未记录拒绝审计: %+v", audits)
	}
}
