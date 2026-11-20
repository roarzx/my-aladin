package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// openapi 是 Agent 侧的命令行入口：`aladdin openapi issue ...`。
//
// 它只做两件事：把命令翻译成 /api/v1/cli/issues 的 HTTP 调用，以及把结果
// 打印成人能看或 Agent 能解析的形态。身份来自机器令牌（ALADDIN_TOKEN），
// Issue 上下文来自 daemon 注入的 ALADDIN_ISSUE_ID / ALADDIN_AGENT_ID。
const openapiUsage = `aladdin openapi — Agent 侧平台接口

用法:
  aladdin openapi issue get [--id <issue-id>]
  aladdin openapi issue list [--keyword <k>] [--status <s>] [--limit 50]
  aladdin openapi issue create --name <标题> [--agent-id <id>]
                                [--requirement-content <需求正文>]
                                [--repository-url <git-url>] [--inform-doc <文档>]
                                [--idempotency-key <key>]
  aladdin openapi issue update-status --id <issue-id> --status <状态> [--reason <原因>]
  aladdin openapi issue input request --message <问题> [--reason <类型>]

状态取值: in_progress | blocked | completed | cancelled | failed
          （也接受 pending / executing / paused / chatting）

通用参数:
  --api-url <url>   控制面地址，默认 ALADDIN_API_URL 或 http://127.0.0.1:8080
  --token <token>   机器令牌，默认 ALADDIN_TOKEN
  --output json|text  输出格式，默认 json

上下文参数未传时依次取 ALADDIN_ISSUE_ID / ALADDIN_AGENT_ID。
`

func runOpenAPI(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, openapiUsage)
		return errors.New("openapi: 缺少子命令")
	}
	switch args[0] {
	case "issue":
		return runOpenAPIIssue(args[1:])
	case "-h", "--help", "help":
		fmt.Print(openapiUsage)
		return nil
	default:
		return fmt.Errorf("openapi: 未知子命令 %q（可用: issue）", args[0])
	}
}

func runOpenAPIIssue(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, openapiUsage)
		return errors.New("openapi issue: 缺少子命令")
	}
	switch args[0] {
	case "get":
		return runIssueGet(args[1:])
	case "list":
		return runIssueList(args[1:])
	case "create":
		return runIssueCreate(args[1:])
	case "update-status":
		return runIssueUpdateStatus(args[1:])
	case "input":
		return runIssueInput(args[1:])
	case "-h", "--help", "help":
		fmt.Print(openapiUsage)
		return nil
	default:
		return fmt.Errorf("openapi issue: 未知子命令 %q", args[0])
	}
}

// ---- HTTP 客户端 ----

type openapiClient struct {
	base  string
	token string
	http  *http.Client
}

// commonFlags 是每个叶子命令共享的连接参数。
type commonFlags struct {
	apiURL string
	token  string
	output string
}

func bindCommon(fs *flag.FlagSet) *commonFlags {
	cf := &commonFlags{}
	fs.StringVar(&cf.apiURL, "api-url", envOr("ALADDIN_API_URL", "http://127.0.0.1:8080"), "控制面地址")
	fs.StringVar(&cf.token, "token", os.Getenv("ALADDIN_TOKEN"), "机器令牌")
	fs.StringVar(&cf.output, "output", "json", "输出格式 json|text")
	return cf
}

func (cf *commonFlags) client() (*openapiClient, error) {
	if strings.TrimSpace(cf.token) == "" {
		return nil, errors.New("缺少机器令牌：--token 或 ALADDIN_TOKEN")
	}
	if cf.output != "json" && cf.output != "text" {
		return nil, fmt.Errorf("--output 只支持 json 或 text，收到 %q", cf.output)
	}
	return &openapiClient{
		base:  strings.TrimRight(cf.apiURL, "/"),
		token: cf.token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *openapiClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求控制面失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return decodeAPIError(resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	return json.Unmarshal(envelope.Data, out)
}

func decodeAPIError(status int, raw []byte) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err == nil && body.Error.Message != "" {
		return fmt.Errorf("%s (HTTP %d)", body.Error.Message, status)
	}
	return fmt.Errorf("控制面返回 HTTP %d", status)
}

// ---- 视图 ----

type issueView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Mode        string    `json:"mode"`
	Version     uint64    `json:"version"`
	Requirement string    `json:"requirement,omitempty"`
	InformDoc   string    `json:"informDoc,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Agents      []struct {
		Ref     string `json:"ref"`
		AgentID string `json:"agentId"`
		Name    string `json:"name"`
		Role    string `json:"role"`
	} `json:"agents,omitempty"`
}

// ---- issue get ----

func runIssueGet(args []string) error {
	fs := flag.NewFlagSet("issue get", flag.ContinueOnError)
	cf := bindCommon(fs)
	id := fs.String("id", os.Getenv("ALADDIN_ISSUE_ID"), "Issue ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*id) == "" {
		return errors.New("issue get: 需要 --id（或 ALADDIN_ISSUE_ID）")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}

	var issue issueView
	if err := client.do(context.Background(), http.MethodGet,
		"/api/v1/cli/issues/"+url.PathEscape(*id), nil, &issue); err != nil {
		return err
	}
	if cf.output == "text" {
		printIssueText(issue)
		return nil
	}
	return printJSON(issue)
}

// ---- issue list ----

func runIssueList(args []string) error {
	fs := flag.NewFlagSet("issue list", flag.ContinueOnError)
	cf := bindCommon(fs)
	keyword := fs.String("keyword", "", "按标题/ID/需求模糊匹配")
	status := fs.String("status", "", "按状态过滤")
	limit := fs.Int("limit", 50, "返回条数上限（1–200）")
	createdStart := fs.String("created-at-start", "", "创建时间起（YYYY-MM-DD[ HH:mm:ss]|RFC3339）")
	createdEnd := fs.String("created-at-end", "", "创建时间止")
	updatedStart := fs.String("updated-at-start", "", "更新时间起")
	updatedEnd := fs.String("updated-at-end", "", "更新时间止")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, err := cf.client()
	if err != nil {
		return err
	}

	query := url.Values{}
	for key, value := range map[string]string{
		"keyword":          *keyword,
		"status":           *status,
		"created-at-start": *createdStart,
		"created-at-end":   *createdEnd,
		"updated-at-start": *updatedStart,
		"updated-at-end":   *updatedEnd,
	} {
		if strings.TrimSpace(value) != "" {
			query.Set(key, value)
		}
	}
	if *limit > 0 {
		query.Set("limit", strconv.Itoa(*limit))
	}

	var issues []issueView
	path := "/api/v1/cli/issues"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := client.do(context.Background(), http.MethodGet, path, nil, &issues); err != nil {
		return err
	}
	if cf.output == "text" {
		if len(issues) == 0 {
			fmt.Fprintln(stdout, "没有匹配的 Issue")
			return nil
		}
		for _, issue := range issues {
			fmt.Fprintf(stdout, "%s  %-10s  %s\n", issue.ID, issue.Status, issue.Name)
		}
		return nil
	}
	return printJSON(issues)
}

// ---- issue create ----

func runIssueCreate(args []string) error {
	fs := flag.NewFlagSet("issue create", flag.ContinueOnError)
	cf := bindCommon(fs)
	name := fs.String("name", "", "Issue 标题")
	agentID := fs.String("agent-id", os.Getenv("ALADDIN_AGENT_ID"), "分配的 Agent ID")
	requirement := fs.String("requirement-content", "", "需求正文")
	informDoc := fs.String("inform-doc", "", "告知规则/补充文档")
	repoURL := fs.String("repository-url", "", "代码仓库地址（需已在工作区登记）")
	idempotencyKey := fs.String("idempotency-key", "", "幂等键：重复提交返回同一个 Issue")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return errors.New("issue create: 需要 --name")
	}
	if strings.TrimSpace(*agentID) == "" {
		return errors.New("issue create: 需要 --agent-id（或 ALADDIN_AGENT_ID）")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":    strings.TrimSpace(*name),
		"agentId": strings.TrimSpace(*agentID),
	}
	if *requirement != "" {
		body["requirementContent"] = *requirement
	}
	if *informDoc != "" {
		body["informDoc"] = *informDoc
	}
	if *repoURL != "" {
		body["repositoryUrl"] = *repoURL
	}
	if *idempotencyKey != "" {
		body["idempotencyKey"] = *idempotencyKey
	}

	var issue issueView
	if err := client.do(context.Background(), http.MethodPost, "/api/v1/cli/issues", body, &issue); err != nil {
		return err
	}
	if cf.output == "text" {
		fmt.Fprintf(stdout, "已创建 Issue %s（状态 %s）\n", issue.ID, issue.Status)
		return nil
	}
	return printJSON(issue)
}

// ---- issue update-status ----

func runIssueUpdateStatus(args []string) error {
	fs := flag.NewFlagSet("issue update-status", flag.ContinueOnError)
	cf := bindCommon(fs)
	id := fs.String("id", os.Getenv("ALADDIN_ISSUE_ID"), "Issue ID")
	status := fs.String("status", "", "目标状态")
	reason := fs.String("reason", "", "状态变更原因")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*id) == "" {
		return errors.New("issue update-status: 需要 --id（或 ALADDIN_ISSUE_ID）")
	}
	if strings.TrimSpace(*status) == "" {
		return errors.New("issue update-status: 需要 --status")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}

	body := map[string]any{"status": *status}
	if *reason != "" {
		body["reason"] = *reason
	}
	var issue issueView
	if err := client.do(context.Background(), http.MethodPost,
		"/api/v1/cli/issues/"+url.PathEscape(*id)+"/status", body, &issue); err != nil {
		return err
	}
	if cf.output == "text" {
		fmt.Fprintf(stdout, "Issue %s 状态已更新为 %s\n", issue.ID, issue.Status)
		return nil
	}
	return printJSON(issue)
}

// ---- issue input request ----

func runIssueInput(args []string) error {
	if len(args) == 0 || args[0] != "request" {
		return errors.New("用法: aladdin openapi issue input request --message <问题> [--reason <类型>]")
	}
	fs := flag.NewFlagSet("issue input request", flag.ContinueOnError)
	cf := bindCommon(fs)
	id := fs.String("id", os.Getenv("ALADDIN_ISSUE_ID"), "Issue ID")
	message := fs.String("message", "", "用户可见的问题")
	reason := fs.String("reason", "", "请求类型，例如 plugin_capability_clarification")
	var options stringList
	fs.Var(&options, "option", "可选项（可重复）")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*id) == "" {
		return errors.New("issue input request: 需要 --id（或 ALADDIN_ISSUE_ID）")
	}
	if strings.TrimSpace(*message) == "" {
		return errors.New("issue input request: 需要 --message")
	}
	client, err := cf.client()
	if err != nil {
		return err
	}

	body := map[string]any{"message": *message, "reason": *reason, "options": []string(options)}
	var created struct {
		ID      string `json:"id"`
		IssueID string `json:"issueId"`
		Status  string `json:"status"`
	}
	if err := client.do(context.Background(), http.MethodPost,
		"/api/v1/cli/issues/"+url.PathEscape(*id)+"/input-requests", body, &created); err != nil {
		return err
	}
	if cf.output == "text" {
		fmt.Fprintf(stdout, "已提交输入请求 %s（%s）\n", created.ID, created.Status)
		return nil
	}
	return printJSON(created)
}

// ---- 输出辅助 ----

// stdout 是命令输出目标；测试里替换成 buffer。
var stdout io.Writer = os.Stdout

// stringList 是可重复出现的字符串参数。
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func printIssueText(issue issueView) {
	fmt.Fprintf(stdout, "ID      %s\n", issue.ID)
	fmt.Fprintf(stdout, "标题    %s\n", issue.Name)
	fmt.Fprintf(stdout, "状态    %s\n", issue.Status)
	fmt.Fprintf(stdout, "模式    %s\n", issue.Mode)
	if issue.Requirement != "" {
		fmt.Fprintf(stdout, "需求    %s\n", truncate(issue.Requirement, 120))
	}
	for _, agent := range issue.Agents {
		fmt.Fprintf(stdout, "Agent   %s %s (%s)\n", agent.Ref, agent.Name, agent.Role)
	}
	fmt.Fprintf(stdout, "更新    %s\n", issue.UpdatedAt.Format(time.RFC3339))
}

func printJSON(v any) error {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func truncate(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
