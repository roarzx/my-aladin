package agentctx

import (
	"context"
	"errors"
	"testing"
)

type fakeClient struct {
	content string
	version int
	puts    int
	// conflictOnce=true 时第一次写入返回冲突（模拟并发写者抢先）。
	conflictOnce bool
	// conflictAll=true 时所有写入都冲突（模拟持续被抢占）。
	conflictAll bool
}

func (f *fakeClient) GetLatest(_ context.Context, _, _, _, _ string) (string, int, error) {
	return f.content, f.version, nil
}

func (f *fakeClient) Put(_ context.Context, _, _, _, _, content string, expectedVersion int) (int, error) {
	f.puts++
	if f.conflictAll {
		return 0, ErrVersionConflict
	}
	if f.conflictOnce {
		f.conflictOnce = false
		return 0, ErrVersionConflict
	}
	if expectedVersion != f.version {
		return 0, ErrVersionConflict
	}
	f.content = content
	f.version++
	return f.version, nil
}

func TestArbitrate(t *testing.T) {
	ctx := context.Background()
	entry := Entry{TaskID: "t", ScopeKind: "task", ScopeID: "t", Kind: "decisions", Content: "v1"}

	client := &fakeClient{}
	version, err := CompareAndPut(ctx, client, entry, 0)
	if err != nil || version != 1 {
		t.Fatalf("CompareAndPut: %d, %v", version, err)
	}

	// 版本不匹配 → 冲突
	if _, err := CompareAndPut(ctx, client, entry, 0); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("期望版本冲突，实际: %v", err)
	}

	// 冲突后重读再合并可成功
	client.content, client.version = "remote", 3
	merged, err := MergeAndPut(ctx, client, Entry{
		TaskID: "t", ScopeKind: "task", ScopeID: "t", Kind: "decisions", Content: "local",
	}, 1, func(remote string) (string, error) {
		return remote + "+local", nil
	})
	if err != nil || merged != 4 {
		t.Fatalf("MergeAndPut: %d, %v", merged, err)
	}
	if client.content != "remote+local" {
		t.Fatalf("合并内容 = %q", client.content)
	}

	// 无 merge 时不得隐式覆盖
	client.content, client.version, client.conflictOnce = "remote", 5, false
	if _, err := MergeAndPut(ctx, client, entry, 1, nil); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("期望版本冲突，实际: %v", err)
	}
}

func TestMergeAndPutConflictAfterMerge(t *testing.T) {
	client := &fakeClient{content: "remote", version: 2, conflictAll: true}
	// 每次写入都被抢占：merge 后重试仍冲突，必须返回冲突而不是覆盖
	_, err := MergeAndPut(context.Background(), client, Entry{
		TaskID: "t", ScopeKind: "task", ScopeID: "t", Kind: "decisions", Content: "local",
	}, 1, func(remote string) (string, error) { return remote, nil })
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("期望最终版本冲突，实际: %v", err)
	}
}
