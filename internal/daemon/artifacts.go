package daemon

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ArtifactUploader 把产物送到控制面（生产实现为 CLIClient，测试可注入）。
type ArtifactUploader interface {
	UploadArtifact(ctx context.Context, executionID, path, contentType string, data []byte) error
}

const (
	artifactMaxFiles = 20
	artifactMaxBytes = 16 << 20
	artifactMaxTotal = 32 << 20
)

// artifactSkipDirs 是扫描工作区时跳过的目录（依赖/缓存/版本库）。
var artifactSkipDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "venv": true,
	"__pycache__": true, ".next": true, ".cache": true, ".idea": true, ".vscode": true,
}

type artifactFile struct {
	relPath     string
	contentType string
	data        []byte
}

// collectArtifacts 收集 since 之后新增或修改的文件（按路径排序）。
//
// 会话式任务通常没有绑定仓库，产物只存在于工作目录里；这里用 mtime 过滤，
// 并对文件数/单文件/总量做上限，避免把依赖目录或大文件推上控制面。
func collectArtifacts(workDir string, since time.Time) []artifactFile {
	if workDir == "" {
		return nil
	}
	cutoff := since.Add(-2 * time.Second)
	out := make([]artifactFile, 0)
	total := 0
	_ = filepath.WalkDir(workDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if artifactSkipDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil || info.ModTime().Before(cutoff) {
			return nil
		}
		if info.Size() > artifactMaxBytes || len(out) >= artifactMaxFiles || total+int(info.Size()) > artifactMaxTotal {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(workDir, path)
		if relErr != nil {
			return nil
		}
		out = append(out, artifactFile{
			relPath:     filepath.ToSlash(rel),
			contentType: contentTypeFor(rel),
			data:        data,
		})
		total += len(data)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].relPath < out[j].relPath })
	return out
}

// contentTypeFor 依据扩展名给出 Content-Type（未识别时按二进制处理）。
func contentTypeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".txt", ".log":
		return "text/plain; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}
