package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"github.com/roarzx/my-aladin/internal/domain"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
)

func runSquad(args []string) error {
	if len(args) == 0 {
		return errors.New("用法: aladdin squad <action|handoff|context|artifact> [flags]")
	}
	switch args[0] {
	case "action":
		return squadActionCmd(args[1:])
	case "handoff":
		return squadHandoffCmd(args[1:])
	case "context":
		return squadContextCmd(args[1:])
	case "artifact":
		return squadArtifactCmd(args[1:])
	default:
		return fmt.Errorf("未知 squad 子命令: %s", args[0])
	}
}

func openDB(dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, errors.New("需要 --dsn 或环境变量 MYSQL_DSN")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 MySQL: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接 MySQL: %w", err)
	}
	return db, nil
}

func squadActionCmd(args []string) error {
	fs := flag.NewFlagSet("squad action", flag.ExitOnError)
	dsn := fs.String("dsn", os.Getenv("MYSQL_DSN"), "MySQL DSN")
	workItemID := fs.String("work-item", "", "工作项 ID")
	action := fs.String("action", "", "start|complete|block|cancel")
	actor := fs.Uint64("actor", 0, "操作人 ID（写入审计事件）")
	_ = fs.Parse(args)

	db, err := openDB(*dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return squadAction(context.Background(), db, *workItemID, *action, *actor, os.Stdout)
}

func squadAction(ctx context.Context, db *sql.DB, workItemID, action string, actor uint64, out io.Writer) error {
	if workItemID == "" {
		return errors.New("squad action: --work-item 必填")
	}
	item, err := mysqlstore.GetWorkItem(ctx, db, workItemID)
	if err != nil {
		return fmt.Errorf("读取工作项: %w", err)
	}
	switch action {
	case "start":
		item, err = mysqlstore.StartWorkItem(ctx, db, workItemID)
	case "complete":
		if item.Status != domain.WorkItemRunning {
			return fmt.Errorf("squad action: 只能完成 running 工作项（当前 %s）", item.Status)
		}
		_, err = mysqlstore.UpdateWorkItemStatus(ctx, db, item.ID,
			domain.WorkItemRunning, domain.WorkItemDone, item.Version)
	case "block":
		if item.Status != domain.WorkItemRunning {
			return fmt.Errorf("squad action: 只能阻塞 running 工作项（当前 %s）", item.Status)
		}
		_, err = mysqlstore.UpdateWorkItemStatus(ctx, db, item.ID,
			domain.WorkItemRunning, domain.WorkItemBlocked, item.Version)
	case "cancel":
		if item.Status.IsTerminal() {
			return fmt.Errorf("squad action: 工作项已结束")
		}
		_, err = mysqlstore.UpdateWorkItemStatus(ctx, db, item.ID,
			item.Status, domain.WorkItemCancelled, item.Version)
	default:
		return fmt.Errorf("squad action: 未知动作 %q", action)
	}
	if err != nil {
		return err
	}
	updated, err := mysqlstore.GetWorkItem(ctx, db, workItemID)
	if err != nil {
		return err
	}
	_, _ = mysqlstore.AppendActivity(ctx, db, item.TaskID, "work_item.action", map[string]any{
		"work_item_id": workItemID, "action": action, "actor": actor,
		"status": string(updated.Status),
	})
	_, err = fmt.Fprintf(out, "%s\t%s\tv%d\n", updated.ID, updated.Status, updated.Version)
	return err
}

func squadHandoffCmd(args []string) error {
	fs := flag.NewFlagSet("squad handoff", flag.ExitOnError)
	dsn := fs.String("dsn", os.Getenv("MYSQL_DSN"), "MySQL DSN")
	workItemID := fs.String("work-item", "", "工作项 ID")
	toAgent := fs.String("to-agent", "", "接收方 Agent ID")
	_ = fs.Parse(args)

	db, err := openDB(*dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return squadHandoff(context.Background(), db, *workItemID, *toAgent, os.Stdout)
}

func squadHandoff(ctx context.Context, db *sql.DB, workItemID, toAgentID string, out io.Writer) error {
	if workItemID == "" || toAgentID == "" {
		return errors.New("squad handoff: --work-item 与 --to-agent 必填")
	}
	item, err := mysqlstore.GetWorkItem(ctx, db, workItemID)
	if err != nil {
		return fmt.Errorf("读取工作项: %w", err)
	}
	if item.Status.IsTerminal() {
		return errors.New("squad handoff: 工作项已结束")
	}
	if _, err := mysqlstore.GetAgent(ctx, db, toAgentID); err != nil {
		return fmt.Errorf("接收方 Agent 不存在: %w", err)
	}
	from := item.OwnerAgentID
	if err := mysqlstore.SetWorkItemOwner(ctx, db, workItemID, toAgentID); err != nil {
		return err
	}
	_, _ = mysqlstore.AppendActivity(ctx, db, item.TaskID, "work_item.handoff", map[string]any{
		"work_item_id": workItemID, "from_agent_id": from, "to_agent_id": toAgentID,
	})
	_, err = fmt.Fprintf(out, "%s\t%s -> %s\n", workItemID, from, toAgentID)
	return err
}

func squadContextCmd(args []string) error {
	fs := flag.NewFlagSet("squad context", flag.ExitOnError)
	dsn := fs.String("dsn", os.Getenv("MYSQL_DSN"), "MySQL DSN")
	taskID := fs.String("task", "", "任务 ID")
	agentID := fs.String("agent", "", "Agent ID")
	_ = fs.Parse(args)

	db, err := openDB(*dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return squadContext(context.Background(), db, *taskID, *agentID, os.Stdout)
}

func squadContext(ctx context.Context, db *sql.DB, taskID, agentID string, out io.Writer) error {
	if taskID == "" || agentID == "" {
		return errors.New("squad context: --task 与 --agent 必填")
	}
	versions, err := mysqlstore.ListContextVersions(ctx, db, taskID, "agent", agentID)
	if err != nil {
		return err
	}
	type entry struct {
		Kind    string `json:"kind"`
		Version int    `json:"version"`
		Content string `json:"content"`
	}
	latest := map[string]int{}
	for _, version := range versions {
		if version.Version > latest[version.Kind] {
			latest[version.Kind] = version.Version
		}
	}
	result := make([]entry, 0, len(latest))
	for kind, version := range latest {
		ctxEntry, err := mysqlstore.GetContextVersion(ctx, db, taskID, "agent", agentID, kind, version)
		if err != nil {
			return err
		}
		result = append(result, entry{Kind: kind, Version: version, Content: ctxEntry.Content})
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(raw))
	return err
}

func squadArtifactCmd(args []string) error {
	fs := flag.NewFlagSet("squad artifact", flag.ExitOnError)
	dsn := fs.String("dsn", os.Getenv("MYSQL_DSN"), "MySQL DSN")
	proposalID := fs.String("proposal", "", "方案 ID")
	offset := fs.Int("offset", 0, "起始字节偏移")
	limit := fs.Int("limit", 4096, "最大字节数")
	_ = fs.Parse(args)

	db, err := openDB(*dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return squadArtifact(context.Background(), db, *proposalID, *offset, *limit, os.Stdout)
}

func squadArtifact(ctx context.Context, db *sql.DB, proposalID string, offset, limit int, out io.Writer) error {
	if proposalID == "" {
		return errors.New("squad artifact: --proposal 必填")
	}
	proposal, err := mysqlstore.GetProposal(ctx, db, proposalID)
	if err != nil {
		return fmt.Errorf("读取方案: %w", err)
	}
	chunk, err := sliceUTF8(proposal.Content, offset, limit)
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, chunk)
	return err
}

// sliceUTF8 按字节区间切片，并把边界对齐到合法 rune 起点，避免输出半个字符。
func sliceUTF8(content string, offset, limit int) (string, error) {
	if offset < 0 || offset > len(content) {
		return "", fmt.Errorf("artifact: offset 越界: %d", offset)
	}
	if limit < 0 {
		return "", fmt.Errorf("artifact: limit 不能为负数")
	}
	start := offset
	for start < len(content) && !utf8.RuneStart(content[start]) {
		start++
	}
	end := len(content)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	for end < len(content) && !utf8.RuneStart(content[end]) {
		end++
	}
	return content[start:end], nil
}
