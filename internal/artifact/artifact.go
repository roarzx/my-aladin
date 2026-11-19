// Package artifact 负责任务产物的落盘与检索（artifacts 表）。
//
// 执行成功后 daemon 把工作区里新产生/修改的文件上传到控制面，控制面按
// <artifact_root>/<task_id>/<sha256><ext> 保存，元信息写入 artifacts 表，
// 供 Issue 详情页展示与下载。
package artifact

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// MaxFileBytes 是单个产物的上限（超过则跳过，不影响任务结果）。
const MaxFileBytes = 16 << 20

// Artifact 是产物元信息。
type Artifact struct {
	ID          string `json:"id"`
	TaskID      string `json:"taskId"`
	StepID      string `json:"stepId,omitempty"`
	AttemptID   string `json:"attemptId,omitempty"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"sizeBytes"`
	ContentType string `json:"contentType"`
}

// Service 提供产物的存取。
type Service struct {
	DB   *sql.DB
	Root string
	Log  *slog.Logger
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Save 保存一份产物；同一任务下相同路径与内容视为幂等重传。
func (s *Service) Save(ctx context.Context, taskID, stepID, attemptID, path, contentType string, data []byte) (Artifact, error) {
	if strings.TrimSpace(taskID) == "" {
		return Artifact{}, errcode.New(errcode.ValidationFailed, "缺少 task_id")
	}
	if strings.TrimSpace(path) == "" {
		return Artifact{}, errcode.New(errcode.ValidationFailed, "缺少 path")
	}
	if len(data) > MaxFileBytes {
		return Artifact{}, errcode.Newf(errcode.ValidationFailed, "产物超过 %d 字节上限", MaxFileBytes)
	}

	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])

	var existing Artifact
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, task_id, COALESCE(step_id,''), COALESCE(attempt_id,''), path, sha256, size_bytes, content_type
		   FROM artifacts WHERE task_id = ? AND path = ? AND sha256 = ? LIMIT 1`,
		taskID, path, digest,
	).Scan(&existing.ID, &existing.TaskID, &existing.StepID, &existing.AttemptID,
		&existing.Path, &existing.SHA256, &existing.SizeBytes, &existing.ContentType)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, err
	}

	filePath := s.filePath(taskID, digest, path)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return Artifact{}, fmt.Errorf("创建产物目录: %w", err)
	}
	if _, statErr := os.Stat(filePath); errors.Is(statErr, os.ErrNotExist) {
		if err := os.WriteFile(filePath, data, 0o644); err != nil {
			return Artifact{}, fmt.Errorf("写入产物: %w", err)
		}
	}

	item := Artifact{
		ID: idgen.New(), TaskID: taskID, StepID: stepID, AttemptID: attemptID,
		Path: path, SHA256: digest, SizeBytes: int64(len(data)), ContentType: contentType,
	}
	if item.ContentType == "" {
		item.ContentType = "application/octet-stream"
	}
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO artifacts (id, task_id, step_id, attempt_id, path, sha256, size_bytes, content_type)
		 VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?)`,
		item.ID, item.TaskID, item.StepID, item.AttemptID,
		item.Path, item.SHA256, item.SizeBytes, item.ContentType,
	); err != nil {
		return Artifact{}, err
	}
	return item, nil
}

// List 返回任务的全部产物（按路径排序）。
func (s *Service) List(ctx context.Context, taskID string) ([]Artifact, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, task_id, COALESCE(step_id,''), COALESCE(attempt_id,''), path, sha256, size_bytes, content_type
		   FROM artifacts WHERE task_id = ? ORDER BY path, id`, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]Artifact, 0)
	for rows.Next() {
		var item Artifact
		if err := rows.Scan(&item.ID, &item.TaskID, &item.StepID, &item.AttemptID,
			&item.Path, &item.SHA256, &item.SizeBytes, &item.ContentType); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Get 返回产物元信息与磁盘路径。
func (s *Service) Get(ctx context.Context, id string) (Artifact, string, error) {
	var item Artifact
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, task_id, COALESCE(step_id,''), COALESCE(attempt_id,''), path, sha256, size_bytes, content_type
		   FROM artifacts WHERE id = ?`, id,
	).Scan(&item.ID, &item.TaskID, &item.StepID, &item.AttemptID,
		&item.Path, &item.SHA256, &item.SizeBytes, &item.ContentType)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, "", errcode.New(errcode.NotFound, "产物不存在")
	}
	if err != nil {
		return Artifact{}, "", err
	}
	return item, s.filePath(item.TaskID, item.SHA256, item.Path), nil
}

// filePath 计算产物在磁盘上的位置；扩展名只保留安全字符。
func (s *Service) filePath(taskID, digest, displayPath string) string {
	ext := filepath.Ext(displayPath)
	if len(ext) > 12 || strings.ContainsAny(ext, `/\`) {
		ext = ""
	}
	for _, r := range ext {
		if !(r == '.' || r == '-' || r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			ext = ""
			break
		}
	}
	return filepath.Join(s.Root, taskID, digest+ext)
}

// Open 打开产物文件（调用方负责关闭）。
func (s *Service) Open(ctx context.Context, id string) (Artifact, io.ReadCloser, error) {
	item, path, err := s.Get(ctx, id)
	if err != nil {
		return Artifact{}, nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Artifact{}, nil, errcode.New(errcode.NotFound, "产物文件缺失")
	}
	return item, file, nil
}
