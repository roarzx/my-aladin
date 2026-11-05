package agentctx

import (
	"context"
	"errors"
	"fmt"
)

// ErrVersionConflict 表示 expectedVersion 与服务端当前版本不一致。
var ErrVersionConflict = errors.New("agentctx: 版本冲突")

// Entry 是一次上下文写入。
type Entry struct {
	TaskID    string
	ScopeKind string
	ScopeID   string
	Kind      string
	Content   string
}

// VersionedClient 是上下文版本服务的最小客户端（服务端 CLI HTTP API / 测试替身）。
type VersionedClient interface {
	GetLatest(ctx context.Context, taskID, scopeKind, scopeID, kind string) (content string, version int, err error)
	Put(ctx context.Context, taskID, scopeKind, scopeID, kind, content string, expectedVersion int) (int, error)
}

// CompareAndPut 以 expectedVersion 做 CAS 写入；版本不匹配返回 ErrVersionConflict。
func CompareAndPut(ctx context.Context, client VersionedClient, entry Entry, expectedVersion int) (int, error) {
	version, err := client.Put(ctx, entry.TaskID, entry.ScopeKind, entry.ScopeID, entry.Kind,
		entry.Content, expectedVersion)
	if err != nil {
		return 0, err
	}
	return version, nil
}

// MergeAndPut 是 E1 的冲突恢复路径：冲突时重读远端、调用 merge 合并，
// 再以远端版本重试一次；仍冲突则返回 ErrVersionConflict（由调用方决定重试节奏）。
//
// merge 为 nil 时冲突直接返回，不做隐式覆盖。
func MergeAndPut(
	ctx context.Context,
	client VersionedClient,
	entry Entry,
	expectedVersion int,
	merge func(remote string) (string, error),
) (int, error) {
	version, err := CompareAndPut(ctx, client, entry, expectedVersion)
	if err == nil {
		return version, nil
	}
	if !errors.Is(err, ErrVersionConflict) || merge == nil {
		return 0, err
	}

	remote, remoteVersion, err := client.GetLatest(ctx, entry.TaskID, entry.ScopeKind, entry.ScopeID, entry.Kind)
	if err != nil {
		return 0, fmt.Errorf("冲突后重读: %w", err)
	}
	merged, err := merge(remote)
	if err != nil {
		return 0, fmt.Errorf("合并远端内容: %w", err)
	}
	entry.Content = merged
	return CompareAndPut(ctx, client, entry, remoteVersion)
}
