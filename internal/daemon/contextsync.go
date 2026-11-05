package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const contextVersionsFile = ".context_versions.json"

type contextVersions struct {
	Versions map[string]int `json:"versions"`
}

// ContextSync 在本地 .meta 目录与 Server 之间同步 decisions / step_summary（C9）。
type ContextSync struct {
	client  *CLIClient
	metaDir string
	path    string

	mu       sync.Mutex
	versions map[string]int
}

// OpenContextSync 加载 .context_versions.json；文件不存在视为空。
func OpenContextSync(metaDir string, client *CLIClient) (*ContextSync, error) {
	if err := os.MkdirAll(metaDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 .meta 目录: %w", err)
	}
	sync := &ContextSync{
		client:   client,
		metaDir:  metaDir,
		path:     filepath.Join(metaDir, contextVersionsFile),
		versions: make(map[string]int),
	}
	data, err := os.ReadFile(sync.path)
	if errors.Is(err, os.ErrNotExist) {
		return sync, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取版本文件: %w", err)
	}
	var file contextVersions
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", sync.path, err)
	}
	if file.Versions != nil {
		sync.versions = file.Versions
	}
	return sync, nil
}

// LocalVersion 返回本地记录的最新版本。
func (s *ContextSync) LocalVersion(kind, scopeID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.versions[versionKey(kind, scopeID)]
}

// Pull 从 Server 拉取最新内容并更新本地版本。
func (s *ContextSync) Pull(ctx context.Context, taskID, scope, scopeID, kind string) (string, int, error) {
	content, version, err := s.client.GetLatest(ctx, taskID, scope, scopeID, kind)
	if err != nil {
		return "", 0, err
	}
	if version > 0 {
		if err := s.setVersion(kind, scopeID, version); err != nil {
			return "", 0, err
		}
	}
	return content, version, nil
}

// Push 以本地版本为 expectedVersion 写入；冲突时拉取远端、调用 merge 后重试一次。
func (s *ContextSync) Push(
	ctx context.Context,
	taskID, scope, scopeID, kind, content string,
	merge func(remote string) (string, error),
) (int, error) {
	expected := s.LocalVersion(kind, scopeID)
	version, err := s.client.PutContext(ctx, taskID, scope, scopeID, kind, content, expected)
	if errors.Is(err, ErrContextConflict) && merge != nil {
		remote, remoteVersion, getErr := s.client.GetLatest(ctx, taskID, scope, scopeID, kind)
		if getErr != nil {
			return 0, getErr
		}
		merged, mergeErr := merge(remote)
		if mergeErr != nil {
			return 0, mergeErr
		}
		version, err = s.client.PutContext(ctx, taskID, scope, scopeID, kind, merged, remoteVersion)
	}
	if err != nil {
		return 0, err
	}
	if err := s.setVersion(kind, scopeID, version); err != nil {
		return 0, err
	}
	return version, nil
}

func (s *ContextSync) setVersion(kind, scopeID string, version int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[versionKey(kind, scopeID)] = version
	return s.saveLocked()
}

func (s *ContextSync) saveLocked() error {
	data, err := json.MarshalIndent(contextVersions{Versions: s.versions}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, append(data, '\n'), 0o600)
}

// versionKey 与参考实现一致：decisions/<task-id>、summary/<step-id>。
func versionKey(kind, scopeID string) string {
	return kind + "/" + scopeID
}
