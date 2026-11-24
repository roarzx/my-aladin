// Package issue 负责 Issue 的一轮执行：把一条消息交给 Agent，并收集回执。
//
// 新模型下一个 Issue 是一个长会话：用户点「开始」或发一条消息，就产生一轮
// 执行（issue_runs）；Agent 在会话里自主推进，并在合适的时机用
// `aladdin openapi issue update-status` 上报状态。平台不再驱动状态机，
// 一轮结束也不会替 Agent 认定「完成」。
package issue

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	"github.com/roarzx/my-aladin/internal/machine"
	"github.com/roarzx/my-aladin/internal/runtime"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Service 编排「一轮执行」。
type Service struct {
	DB      *sql.DB
	Runtime runtime.Runtime
	Log     *slog.Logger
}

// StartTurn 为 Issue 创建一轮执行并派发到 Agent 所在机器。
//
// 派发成功后才把任务置为 executing；派发失败会回滚到之前的状态，
// 避免「平台说在执行，其实没人干活」。
func (s *Service) StartTurn(ctx context.Context, taskID, prompt string) (domain.IssueRun, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return domain.IssueRun{}, errcode.New(errcode.ValidationFailed, "提示词不能为空")
	}

	task, err := mysqlstore.GetTask(ctx, s.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IssueRun{}, errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return domain.IssueRun{}, errcode.New(errcode.UpstreamUnavailable, "读取任务失败")
	}
	if task.Status.IsTerminal() {
		return domain.IssueRun{}, errcode.Newf(errcode.Conflict, "任务已结束（%s），不能再执行", task.Status)
	}
	if _, active, err := mysqlstore.ActiveIssueRun(ctx, s.DB, taskID); err != nil {
		return domain.IssueRun{}, errcode.New(errcode.UpstreamUnavailable, "读取执行状态失败")
	} else if active {
		return domain.IssueRun{}, errcode.New(errcode.Conflict, "已有执行进行中，请等待本轮结束")
	}

	agentID, err := mysqlstore.GetPlannerAgentID(ctx, s.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IssueRun{}, errcode.New(errcode.NotFound, "任务没有可执行的 Agent")
	}
	if err != nil {
		return domain.IssueRun{}, errcode.New(errcode.UpstreamUnavailable, "读取 Agent 失败")
	}
	agentConfig, err := mysqlstore.GetAgent(ctx, s.DB, agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IssueRun{}, errcode.New(errcode.AgentUnavailable, "Agent 不存在")
	}
	if err != nil {
		return domain.IssueRun{}, errcode.New(errcode.UpstreamUnavailable, "读取 Agent 失败")
	}
	if err := agentConfig.SchedulableBy(task.CreatedBy); err != nil {
		return domain.IssueRun{}, err
	}

	m, err := mysqlstore.GetMachine(ctx, s.DB, agentConfig.MachineID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IssueRun{}, errcode.New(errcode.AgentUnavailable, "Agent 绑定的机器不存在")
	}
	if err != nil {
		return domain.IssueRun{}, errcode.New(errcode.UpstreamUnavailable, "读取机器失败")
	}
	if m.Status != machine.StatusOnline {
		return domain.IssueRun{}, errcode.New(errcode.AgentUnavailable, "Agent 所在机器不在线")
	}
	if s.Runtime == nil {
		return domain.IssueRun{}, errcode.New(errcode.InternalError, "执行通道未配置")
	}

	sessionRecord, err := mysqlstore.EnsureSession(ctx, s.DB, taskID, agentID, "issue")
	if err != nil {
		return domain.IssueRun{}, errcode.New(errcode.UpstreamUnavailable, "创建会话失败")
	}

	// 任务可能绑定多个仓库，当前只使用第一个（按 URL 排序）。
	repos, err := mysqlstore.ListTaskRepositories(ctx, s.DB, taskID)
	if err != nil {
		return domain.IssueRun{}, errcode.New(errcode.UpstreamUnavailable, "读取任务仓库失败")
	}
	var repo mysqlstore.TaskRepository
	if len(repos) > 0 {
		repo = repos[0]
	}

	now := time.Now().UTC()
	run := domain.IssueRun{
		ID: idgen.New(), TaskID: taskID, AgentID: agentID,
		SessionID: sessionRecord.ID, ExecutionID: idgen.New(),
		Status: domain.RunDispatched, CreatedAt: now, UpdatedAt: now,
	}
	if err := mysqlstore.InsertIssueRun(ctx, s.DB, run); err != nil {
		return domain.IssueRun{}, errcode.New(errcode.UpstreamUnavailable, "写入执行记录失败")
	}

	prevStatus := task.Status
	if prevStatus != domain.TaskExecuting {
		if err := setStatus(ctx, s.DB, &task, domain.TaskExecuting, "开始执行", "platform"); err != nil {
			_, _ = mysqlstore.FinishIssueRun(ctx, s.DB, run.ID,
				[]domain.RunStatus{domain.RunDispatched}, domain.RunFailed, "", "internal", err.Error())
			return domain.IssueRun{}, err
		}
	}

	_, dispatchErr := s.Runtime.Dispatch(ctx, runtime.DispatchRequest{
		ExecutionID:       run.ExecutionID,
		MachineID:         agentConfig.MachineID,
		IssueID:           taskID,
		AgentID:           agentID,
		SessionID:         sessionRecord.ID,
		ACPSessionID:      sessionRecord.ACPSessionID,
		Prompt:            prompt,
		SystemPrompt:      agentConfig.SystemPrompt,
		EvalMode:          agentConfig.EvalMode,
		WorkDir:           agentConfig.WorkDir,
		RepoURL:           repo.URL,
		RepoDefaultBranch: repo.DefaultBranch,
		Branch:            task.PlanBranch,
		IssueTitle:        task.Name,
	})
	if dispatchErr != nil {
		_, _ = mysqlstore.FinishIssueRun(ctx, s.DB, run.ID,
			[]domain.RunStatus{domain.RunDispatched}, domain.RunFailed, "", "dispatch", dispatchErr.Error())
		if prevStatus != domain.TaskExecuting {
			_ = setStatus(ctx, s.DB, &task, prevStatus, "派发失败，回滚状态", "platform")
		}
		return domain.IssueRun{}, errcode.Newf(errcode.RuntimeStartFailed, "派发失败: %v", dispatchErr)
	}

	_, _ = mysqlstore.AppendActivity(ctx, s.DB, taskID, "turn.started", map[string]any{
		"run_id": run.ID, "execution_id": run.ExecutionID, "agent_id": agentID,
	})
	s.log().Info("Issue 执行已派发", "issue", taskID, "run", run.ID,
		"execution", run.ExecutionID, "agent", agentID)
	return run, nil
}

// StartFromRequirement 用 Issue 自带的需求开始第一轮（创建后自动执行）。
func (s *Service) StartFromRequirement(ctx context.Context, taskID string) (domain.IssueRun, error) {
	task, err := mysqlstore.GetTask(ctx, s.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IssueRun{}, errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return domain.IssueRun{}, errcode.New(errcode.UpstreamUnavailable, "读取任务失败")
	}
	prompt := task.Requirement
	if strings.TrimSpace(prompt) == "" {
		prompt = task.Name
	}
	if task.InformDoc != "" {
		prompt += "\n\n补充说明：\n" + task.InformDoc
	}
	return s.StartTurn(ctx, taskID, prompt)
}

// Message 把用户的一条消息交给 Agent，形成新一轮执行。
func (s *Service) Message(ctx context.Context, taskID, content string) (domain.IssueRun, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return domain.IssueRun{}, errcode.New(errcode.ValidationFailed, "消息内容为空")
	}
	if len([]rune(content)) > MaxMessageRunes {
		return domain.IssueRun{}, errcode.Newf(errcode.ValidationFailed,
			"消息超过 %d 字", MaxMessageRunes)
	}
	// 执行中直接拒绝：消息一旦落库就会永久留在会话里，但这一轮不会去执行它，
	// 会话会出现「用户说了一句话，Agent 从未回应」的假象。
	if _, active, err := mysqlstore.ActiveIssueRun(ctx, s.DB, taskID); err != nil {
		return domain.IssueRun{}, errcode.New(errcode.UpstreamUnavailable, "读取执行状态失败")
	} else if active {
		return domain.IssueRun{}, errcode.New(errcode.Conflict, "已有执行进行中，这条消息没有发送")
	}
	// 先落用户消息，保证会话顺序在 turn.started 之前。
	if _, err := mysqlstore.AppendActivity(ctx, s.DB, taskID, "message.user",
		map[string]any{"text": content}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.IssueRun{}, errcode.New(errcode.NotFound, "任务不存在")
		}
		return domain.IssueRun{}, errcode.New(errcode.UpstreamUnavailable, "写入消息失败")
	}
	return s.StartTurn(ctx, taskID, content)
}

// Cancel 停止当前执行并把任务置为已取消。
func (s *Service) Cancel(ctx context.Context, taskID, reason string) error {
	task, err := mysqlstore.GetTask(ctx, s.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return errcode.New(errcode.UpstreamUnavailable, "读取任务失败")
	}

	run, active, err := mysqlstore.ActiveIssueRun(ctx, s.DB, taskID)
	if err != nil {
		return errcode.New(errcode.UpstreamUnavailable, "读取执行状态失败")
	}
	acknowledged := false
	if active && s.Runtime != nil {
		acknowledged, _ = s.Runtime.Cancel(ctx, run.ExecutionID)
		_, _ = mysqlstore.FinishIssueRun(ctx, s.DB, run.ID,
			[]domain.RunStatus{domain.RunDispatched, domain.RunRunning}, domain.RunCancelled, "", "", "")
	}

	if !task.Status.IsTerminal() {
		if err := setStatus(ctx, s.DB, &task, domain.TaskCancelled, reason, "user"); err != nil {
			return err
		}
	}
	_, _ = mysqlstore.AppendActivity(ctx, s.DB, taskID, "turn.cancelled", map[string]any{
		"run_id": run.ID, "acknowledged": acknowledged, "reason": reason,
	})
	return nil
}

// ApplyTurnDone 处理 daemon 的 turn_done。
//
// 只记录本轮输出与会话游标，不改任务状态：是否完成由 Agent 用 CLI 决定。
func (s *Service) ApplyTurnDone(ctx context.Context, executionID string, data map[string]any) error {
	run, ok, err := s.run(executionID)
	if err != nil || !ok {
		return err
	}

	reply := stringField(data, "reply")
	if _, err := mysqlstore.FinishIssueRun(ctx, s.DB, run.ID,
		[]domain.RunStatus{domain.RunDispatched, domain.RunRunning},
		domain.RunCompleted, reply, "", ""); err != nil {
		return err
	}
	// 用量快照：统计与看板直接按列聚合，不再反解活动 JSON。
	model := ""
	if run.AgentID != "" {
		if agent, err := mysqlstore.GetAgent(ctx, s.DB, run.AgentID); err == nil {
			model = agent.Model
		}
	}
	if err := mysqlstore.SetIssueRunUsage(ctx, s.DB, run.ID,
		int64(intField(data, "input_tokens")), int64(intField(data, "output_tokens")),
		model); err != nil {
		s.log().Warn("写入执行用量失败", "run", run.ID, "err", err)
	}
	if acpID := stringField(data, "acp_session_id"); acpID != "" && run.SessionID != "" {
		if err := mysqlstore.SetSessionACPID(ctx, s.DB, run.SessionID, acpID, ""); err != nil {
			s.log().Warn("保存 ACP 会话失败", "session", run.SessionID, "err", err)
		}
	}
	// 过程说明先落库：它属于执行过程，展示在记录页。
	if narration := stringField(data, "narration"); narration != "" {
		_, _ = mysqlstore.AppendActivity(ctx, s.DB, run.TaskID, "agent.narration", map[string]any{
			"text": narration, "run_id": run.ID,
		})
	}
	if reply != "" {
		_, _ = mysqlstore.AppendActivity(ctx, s.DB, run.TaskID, "message.assistant", map[string]any{
			"text": reply, "run_id": run.ID,
		})
	}
	// 评测任务不追加平台上报提示词，Agent 不会自己报状态；一轮结束即进入
	// 「等待验收」，判分结果由外部 harness 回填。
	s.pauseEvalIssue(ctx, run)
	_, _ = mysqlstore.AppendActivity(ctx, s.DB, run.TaskID, "turn.done", map[string]any{
		"run_id":        run.ID,
		"duration_ms":   intField(data, "duration_ms"),
		"input_tokens":  intField(data, "input_tokens"),
		"output_tokens": intField(data, "output_tokens"),
		"commit_sha":    stringField(data, "commit_sha"),
		"pr_url":        stringField(data, "pr_url"),
	})
	return nil
}

// ApplyTurnFail 处理 daemon 的 turn_fail：只记录失败，不改任务状态。
func (s *Service) ApplyTurnFail(ctx context.Context, executionID string, data map[string]any) error {
	run, ok, err := s.run(executionID)
	if err != nil || !ok {
		return err
	}
	reason := stringField(data, "reason")
	errorClass := stringField(data, "error_class")
	if _, err := mysqlstore.FinishIssueRun(ctx, s.DB, run.ID,
		[]domain.RunStatus{domain.RunDispatched, domain.RunRunning},
		domain.RunFailed, "", errorClass, reason); err != nil {
		return err
	}
	_, _ = mysqlstore.AppendActivity(ctx, s.DB, run.TaskID, "turn.failed", map[string]any{
		"run_id": run.ID, "reason": reason, "error_class": errorClass,
		"uncertain": data["uncertain"],
	})
	return nil
}

// FailStaleRuns 把某台机器上没收尾的执行标记为失败。
//
// daemon 重启（或进程被杀）后，那些执行不会再回来；不清掉的话
// 对应 Issue 会一直「已有执行进行中」，连消息都发不出去。
func (s *Service) FailStaleRuns(ctx context.Context, machineID string) int {
	runs, err := mysqlstore.ListActiveRunsByMachine(ctx, s.DB, machineID)
	if err != nil {
		s.log().Warn("清理未收尾执行失败", "machine", machineID, "err", err)
		return 0
	}
	reason := "执行进程已不在（daemon 重启），本轮中断"
	failed := 0
	for _, run := range runs {
		ok, err := mysqlstore.FinishIssueRun(ctx, s.DB, run.ID,
			[]domain.RunStatus{domain.RunDispatched, domain.RunRunning},
			domain.RunFailed, "", "runtime", reason)
		if err != nil || !ok {
			continue
		}
		failed++
		_, _ = mysqlstore.AppendActivity(ctx, s.DB, run.TaskID, "turn.failed", map[string]any{
			"run_id": run.ID, "reason": reason, "error_class": "runtime", "uncertain": true,
		})
	}
	if failed > 0 {
		s.log().Info("已清理未收尾执行", "machine", machineID, "count", failed)
	}
	return failed
}

// pauseEvalIssue 把评测 Issue 从「执行中」落到「等待验收」。
func (s *Service) pauseEvalIssue(ctx context.Context, run domain.IssueRun) {
	agentConfig, err := mysqlstore.GetAgent(ctx, s.DB, run.AgentID)
	if err != nil || !agentConfig.EvalMode {
		return
	}
	task, err := mysqlstore.GetTask(ctx, s.DB, run.TaskID)
	if err != nil || task.Status != domain.TaskExecuting {
		return
	}
	if err := setStatus(ctx, s.DB, &task, domain.TaskPaused, "本轮结束，等待验收", "platform"); err != nil {
		s.log().Warn("评测 Issue 状态落库失败", "issue", run.TaskID, "err", err)
	}
}

// MarkRunning 在收到首帧流式输出时把执行推进为 running。
func (s *Service) MarkRunning(ctx context.Context, executionID string) {
	if err := mysqlstore.MarkRunning(ctx, s.DB, executionID); err != nil {
		s.log().Warn("更新执行状态失败", "execution", executionID, "err", err)
	}
}

func (s *Service) run(executionID string) (domain.IssueRun, bool, error) {
	repo, err := mysqlstore.GetIssueRunByExecution(context.Background(), s.DB, executionID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IssueRun{}, false, nil
	}
	if err != nil {
		return domain.IssueRun{}, false, err
	}
	return repo, true, nil
}

// MaxMessageRunes 是单条用户消息的长度上限。
const MaxMessageRunes = 8000

// setStatus 以乐观锁写入状态并记录活动。
func setStatus(ctx context.Context, db *sql.DB, task *domain.Task, next domain.TaskStatus,
	reason, source string) error {
	from, version := task.Status, task.Version
	if err := task.ReportStatus(next); err != nil {
		return errcode.New(errcode.Conflict, err.Error())
	}
	if from == next {
		return nil
	}
	if _, err := mysqlstore.UpdateTaskState(ctx, db, task.ID, from, next, version); err != nil {
		return err
	}
	_, _ = mysqlstore.AppendActivity(ctx, db, task.ID, "issue.status", map[string]any{
		"from": string(from), "to": string(next), "reason": reason, "source": source,
	})
	return nil
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func stringField(data map[string]any, key string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

func intField(data map[string]any, key string) int {
	switch v := data[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}
