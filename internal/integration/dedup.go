// Package integration 负责外部需求（TAPD / Process）的抓取与去重建单（G6）。
package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Source 是外部需求来源。
type Source string

const (
	SourceTAPD    Source = "tapd"
	SourceProcess Source = "process"
)

// Requirement 是抓取到的外部需求。正文只作为任务描述，不携带任何执行策略。
type Requirement struct {
	Source Source
	URL    string
	Title  string
	Body   string
}

// Fetcher 抓取外部需求。
type Fetcher interface {
	Fetch(ctx context.Context, source Source, url string) (Requirement, error)
}

// DedupStore 记录 (来源, URL) → task_id。
type DedupStore interface {
	Lookup(ctx context.Context, source Source, url string) (taskID string, found bool, err error)
	Save(ctx context.Context, source Source, url, taskID string) error
}

// ErrSourceUnsupported 表示来源类型不支持。
var ErrSourceUnsupported = errors.New("integration: 不支持的需求来源")

// EnsureTask 按 (source, url) 去重创建任务；created=false 表示复用已有任务。
//
// 外部正文只经 Sanitize 后传给 create，调用方不得从正文解析执行配置，
// 执行策略来自任务模板/快照（dev_spec 3.5）。
func EnsureTask(ctx context.Context, store DedupStore, fetcher Fetcher,
	source Source, url string, create func(Requirement) (string, error)) (string, bool, error) {
	if source != SourceTAPD && source != SourceProcess {
		return "", false, ErrSourceUnsupported
	}
	if strings.TrimSpace(url) == "" {
		return "", false, errors.New("integration: url 必填")
	}
	if existing, found, err := store.Lookup(ctx, source, url); err != nil {
		return "", false, err
	} else if found {
		return existing, false, nil
	}

	requirement, err := fetcher.Fetch(ctx, source, url)
	if err != nil {
		return "", false, fmt.Errorf("抓取需求: %w", err)
	}
	requirement.Source = source
	requirement.URL = url
	requirement.Title = Sanitize(requirement.Title, 256)
	requirement.Body = Sanitize(requirement.Body, 8<<10)
	if requirement.Title == "" {
		return "", false, errors.New("integration: 需求标题为空")
	}

	taskID, err := create(requirement)
	if err != nil {
		return "", false, err
	}
	if err := store.Save(ctx, source, url, taskID); err != nil {
		return "", false, err
	}
	return taskID, true, nil
}

// Sanitize 去掉控制字符、折叠空白并截断，避免日志注入与超长正文。
func Sanitize(text string, maxBytes int) string {
	var builder strings.Builder
	lastSpace := false
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			continue
		}
		if unicode.IsSpace(r) {
			if lastSpace {
				continue
			}
			lastSpace = true
			builder.WriteByte(' ')
			continue
		}
		lastSpace = false
		builder.WriteRune(r)
	}
	cleaned := strings.TrimSpace(builder.String())
	if maxBytes > 0 && len(cleaned) > maxBytes {
		cleaned = strings.ToValidUTF8(cleaned[:maxBytes], "")
	}
	return cleaned
}

// URLHash 返回去重索引使用的稳定哈希。
func URLHash(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}
