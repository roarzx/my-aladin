package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxRequirementBytes = 1 << 20

// HTTPFetcher 按来源调用外部接口，兼容 TAPD / Process 的常见字段。
type HTTPFetcher struct {
	Client  *http.Client
	BaseURL string
	// Path 按来源拼接，例如 TAPD: /tapd/story?url=，Process: /process/requirement?url=
	Path map[Source]string
}

// NewHTTPFetcher 构造抓取器。
func NewHTTPFetcher(baseURL string, paths map[Source]string) *HTTPFetcher {
	return &HTTPFetcher{
		Client:  &http.Client{Timeout: 10 * time.Second},
		BaseURL: baseURL,
		Path:    paths,
	}
}

// Fetch 实现 Fetcher。
func (f *HTTPFetcher) Fetch(ctx context.Context, source Source, url string) (Requirement, error) {
	path, ok := f.Path[source]
	if !ok {
		return Requirement{}, fmt.Errorf("%w: %s", ErrSourceUnsupported, source)
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		f.BaseURL+path+urlPathEscape(url), nil)
	if err != nil {
		return Requirement{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Requirement{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return Requirement{}, fmt.Errorf("外部接口返回 %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRequirementBytes))
	if err != nil {
		return Requirement{}, err
	}
	var body struct {
		Title       string `json:"title"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Requirement{}, fmt.Errorf("解析需求: %w", err)
	}
	title := body.Title
	if title == "" {
		title = body.Name
	}
	content := body.Description
	if content == "" {
		content = body.Body
	}
	return Requirement{Source: source, URL: url, Title: title, Body: content}, nil
}

func urlPathEscape(url string) string {
	// 保留原始 URL，由调用方决定编码；这里只做最小转义避免破坏 query
	out := make([]byte, 0, len(url))
	for i := 0; i < len(url); i++ {
		c := url[i]
		if c == ' ' {
			out = append(out, '+')
			continue
		}
		out = append(out, c)
	}
	return string(out)
}
