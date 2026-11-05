// Package plan 负责计划提交、版本、确认与启动（D5/D6）。
package plan

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// StepInput 是提交计划时的步骤。
type StepInput struct {
	Ordinal  int    `json:"ordinal"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	AgentRef string `json:"agentRef"`
}

// Proposal 对应 POST /issues/{id}/plans。
type Proposal struct {
	ContentMD   string      `json:"contentMd"`
	Revision    int         `json:"revision"`
	ContentHash string      `json:"contentHash"`
	Steps       []StepInput `json:"steps"`
}

// Service 是计划领域服务。
type Service struct {
	DB *sql.DB
}

// Propose 保存新的计划版本；旧版本保留。
func (s *Service) Propose(ctx context.Context, taskID string, req Proposal) (domain.Plan, []domain.Step, error) {
	if err := validateProposal(req); err != nil {
		return domain.Plan{}, nil, err
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Plan{}, nil, errcode.New(errcode.UpstreamUnavailable, "开启事务失败")
	}
	defer func() { _ = tx.Rollback() }()

	task, err := mysqlstore.GetTask(ctx, tx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, nil, errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return domain.Plan{}, nil, errcode.New(errcode.UpstreamUnavailable, "读取任务失败")
	}

	var event domain.Event
	switch task.Status {
	case domain.TaskPending:
		event = domain.EvPlanSubmitted
	case domain.TaskPlanConfirming:
		event = domain.EvPlanRevised
	case domain.TaskChatting:
		event = domain.EvPlanAppended
	default:
		return domain.Plan{}, nil, errcode.Newf(errcode.Conflict, "任务状态 %s 不接受计划", task.Status)
	}

	latest, err := mysqlstore.LatestPlanRevision(ctx, tx, taskID)
	if err != nil {
		return domain.Plan{}, nil, errcode.New(errcode.UpstreamUnavailable, "读取计划版本失败")
	}
	if req.Revision != 0 && req.Revision != latest+1 {
		return domain.Plan{}, nil, errcode.Newf(errcode.RevisionConflict,
			"期望 revision=%d，实际提交 %d", latest+1, req.Revision)
	}

	agents, err := mysqlstore.GetTaskAgents(ctx, tx, taskID)
	if err != nil {
		return domain.Plan{}, nil, errcode.New(errcode.UpstreamUnavailable, "读取任务成员失败")
	}
	for _, step := range req.Steps {
		if _, ok := agents[step.AgentRef]; !ok {
			return domain.Plan{}, nil, errcode.Newf(errcode.AgentRefInvalid,
				"agent_ref 不在任务白名单内: %s", step.AgentRef)
		}
	}

	hash := req.ContentHash
	if hash == "" {
		sum := sha256.Sum256([]byte(req.ContentMD))
		hash = hex.EncodeToString(sum[:])
	}

	plan := domain.Plan{
		ID: idgen.New(), TaskID: taskID, Revision: latest + 1,
		ContentHash: hash, ContentMD: req.ContentMD, Status: domain.PlanIdle,
	}
	if err := mysqlstore.InsertPlan(ctx, tx, plan); err != nil {
		return domain.Plan{}, nil, errcode.New(errcode.UpstreamUnavailable, "写入计划失败")
	}

	steps := make([]domain.Step, 0, len(req.Steps))
	for _, input := range req.Steps {
		step := domain.Step{
			ID: idgen.New(), PlanID: plan.ID, Ordinal: input.Ordinal,
			Title: input.Title, Body: input.Body, AgentRef: input.AgentRef,
			Status: domain.StepPending,
		}
		if err := mysqlstore.InsertPlanStep(ctx, tx, step); err != nil {
			return domain.Plan{}, nil, errcode.New(errcode.UpstreamUnavailable, "写入计划步骤失败")
		}
		steps = append(steps, step)
	}

	before, version := task.Status, task.Version
	if err := task.Apply(event); err != nil {
		return domain.Plan{}, nil, errcode.Newf(errcode.Conflict, "任务状态不允许提交计划: %v", err)
	}
	if _, err := mysqlstore.UpdateTaskState(ctx, tx, taskID, before, task.Status, version); err != nil {
		return domain.Plan{}, nil, err
	}

	if err := tx.Commit(); err != nil {
		return domain.Plan{}, nil, errcode.New(errcode.UpstreamUnavailable, "提交事务失败")
	}
	return plan, steps, nil
}

// Confirm 记录对指定版本的确认；确认不可移用于新版本。
func (s *Service) Confirm(ctx context.Context, planID string, revision int, contentHash string, actorID uint64) (domain.Confirmation, error) {
	if revision <= 0 {
		return domain.Confirmation{}, errcode.New(errcode.ValidationFailed, "revision 必须大于 0")
	}

	plan, _, err := mysqlstore.GetPlanByID(ctx, s.DB, planID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Confirmation{}, errcode.New(errcode.NotFound, "计划不存在")
	}
	if err != nil {
		return domain.Confirmation{}, errcode.New(errcode.UpstreamUnavailable, "读取计划失败")
	}
	if plan.Revision != revision {
		return domain.Confirmation{}, errcode.Newf(errcode.RevisionConflict,
			"计划 revision=%d，提交 %d", plan.Revision, revision)
	}
	if contentHash != "" && plan.ContentHash != contentHash {
		return domain.Confirmation{}, errcode.New(errcode.HashMismatch, "contentHash 与计划不一致")
	}

	latest, err := mysqlstore.LatestPlanRevision(ctx, s.DB, plan.TaskID)
	if err != nil {
		return domain.Confirmation{}, errcode.New(errcode.UpstreamUnavailable, "读取计划版本失败")
	}
	if plan.Revision != latest {
		return domain.Confirmation{}, errcode.Newf(errcode.PlanSuperseded,
			"最新 revision=%d，提交 %d", latest, plan.Revision)
	}

	confirmation, err := mysqlstore.InsertConfirmation(ctx, s.DB, planID, revision, plan.ContentHash, actorID)
	if err != nil {
		return domain.Confirmation{}, errcode.New(errcode.UpstreamUnavailable, "写入确认失败")
	}
	return confirmation, nil
}

// Start 在确认有效时把任务推进到 executing（启动不等于确认）。
func (s *Service) Start(ctx context.Context, taskID string, expectedVersion uint64) (domain.Task, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, errcode.New(errcode.UpstreamUnavailable, "开启事务失败")
	}
	defer func() { _ = tx.Rollback() }()

	task, err := mysqlstore.GetTask(ctx, tx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return domain.Task{}, errcode.New(errcode.UpstreamUnavailable, "读取任务失败")
	}
	if expectedVersion != 0 && task.Version != expectedVersion {
		return domain.Task{}, errcode.Newf(errcode.Conflict,
			"任务版本已变化: 当前 %d，期望 %d", task.Version, expectedVersion)
	}
	if task.Status != domain.TaskPlanConfirming {
		return domain.Task{}, errcode.Newf(errcode.Conflict, "任务状态 %s 不可启动", task.Status)
	}

	latest, err := mysqlstore.LatestPlanRevision(ctx, tx, taskID)
	if err != nil {
		return domain.Task{}, errcode.New(errcode.UpstreamUnavailable, "读取计划版本失败")
	}
	if latest == 0 {
		return domain.Task{}, errcode.New(errcode.NotConfirmed, "计划尚未提交")
	}
	plan, _, err := mysqlstore.GetPlanByRevision(ctx, tx, taskID, latest)
	if err != nil {
		return domain.Task{}, errcode.New(errcode.UpstreamUnavailable, "读取计划失败")
	}
	confirmation, err := mysqlstore.GetConfirmation(ctx, tx, plan.ID, plan.Revision)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && confirmation.ContentHash != plan.ContentHash) {
		return domain.Task{}, errcode.New(errcode.NotConfirmed, "当前计划版本尚未确认")
	}
	if err != nil {
		return domain.Task{}, errcode.New(errcode.UpstreamUnavailable, "读取确认失败")
	}

	if _, err := mysqlstore.UpdatePlanState(ctx, tx, plan.ID, plan.Status, domain.PlanExecuting, plan.Version); err != nil {
		return domain.Task{}, err
	}

	before, version := task.Status, task.Version
	if err := task.Apply(domain.EvPlanConfirmedAndStarted); err != nil {
		return domain.Task{}, errcode.Newf(errcode.Conflict, "任务状态不允许启动: %v", err)
	}
	newVersion, err := mysqlstore.UpdateTaskState(ctx, tx, taskID, before, task.Status, version)
	if err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, errcode.New(errcode.UpstreamUnavailable, "提交事务失败")
	}
	task.Version = newVersion
	return task, nil
}

func validateProposal(req Proposal) error {
	if strings.TrimSpace(req.ContentMD) == "" {
		return errcode.New(errcode.ValidationFailed, "contentMd 不能为空")
	}
	if len(req.Steps) == 0 {
		return errcode.New(errcode.ValidationFailed, "steps 不能为空")
	}
	seen := make(map[int]bool, len(req.Steps))
	for _, step := range req.Steps {
		if step.Ordinal <= 0 || seen[step.Ordinal] {
			return errcode.Newf(errcode.ValidationFailed, "ordinal 必须唯一且大于 0: %d", step.Ordinal)
		}
		seen[step.Ordinal] = true
		if strings.TrimSpace(step.Title) == "" || strings.TrimSpace(step.AgentRef) == "" {
			return fmt.Errorf("%w", errcode.New(errcode.ValidationFailed, "步骤 title 与 agentRef 必填"))
		}
	}
	return nil
}
