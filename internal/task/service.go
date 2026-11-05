package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// CreateRequest 对应 9.6 的 POST /workspaces/{wid}/issues 请求体。
type CreateRequest struct {
	Name           string          `json:"name"`
	Mode           domain.TaskMode `json:"mode"`
	TemplateID     string          `json:"templateId"`
	AgentIDs       []string        `json:"agentIds"`
	PlannerAgentID string          `json:"plannerAgentId"`
	Repositories   []string        `json:"repositories"`
}

// Service 负责任务创建：模板展开、成员校验与快照落库。
type Service struct {
	DB *sql.DB
}

type agentRef struct {
	Ref     string `json:"ref"`
	AgentID string `json:"agentId"`
	Role    string `json:"role"`
}

type snapshot struct {
	Template       *templateRef    `json:"template,omitempty"`
	TemplateConfig json.RawMessage `json:"templateConfig,omitempty"`
	Mode           string          `json:"mode"`
	PlannerAgentID string          `json:"plannerAgentId"`
	Agents         []agentRef      `json:"agents"`
	Repositories   []string        `json:"repositories"`
}

// Create 在单个事务内完成：校验 → 展开模板 → 写任务/成员/仓库。
func (s *Service) Create(ctx context.Context, creatorID uint64, workspaceID string, req CreateRequest) (*domain.Task, error) {
	req, err := normalizeAndValidate(req)
	if err != nil {
		return nil, err
	}
	if s.DB == nil {
		return nil, errcode.New(errcode.UpstreamUnavailable, "数据库未配置")
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, errcode.New(errcode.UpstreamUnavailable, "开启事务失败")
	}
	defer func() { _ = tx.Rollback() }()

	refs := make([]agentRef, 0, len(req.AgentIDs))
	for i, agentID := range req.AgentIDs {
		a, err := mysqlstore.GetAgent(ctx, tx, agentID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errcode.Newf(errcode.AgentUnavailable, "Agent 不存在: %s", agentID)
		}
		if err != nil {
			return nil, errcode.New(errcode.UpstreamUnavailable, "读取 Agent 失败")
		}
		if a.WorkspaceID != workspaceID {
			return nil, errcode.Newf(errcode.AgentUnavailable, "Agent 不属于该工作区: %s", agentID)
		}
		if err := a.SchedulableBy(creatorID); err != nil {
			return nil, err
		}
		refs = append(refs, agentRef{
			Ref:     fmt.Sprintf("a%d", i+1),
			AgentID: a.ID,
			Role:    string(a.TaskRole(req.PlannerAgentID)),
		})
	}

	if err := validateRepositories(ctx, tx, workspaceID, req.Repositories); err != nil {
		return nil, err
	}

	tplRef, tplConfig, err := expandTemplate(ctx, tx, workspaceID, req.TemplateID)
	if err != nil {
		return nil, err
	}

	snap := snapshot{
		Template:       tplRef,
		TemplateConfig: tplConfig,
		Mode:           string(req.Mode),
		PlannerAgentID: req.PlannerAgentID,
		Agents:         refs,
		Repositories:   req.Repositories,
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return nil, errcode.New(errcode.InternalError, "生成配置快照失败")
	}

	now := time.Now().UTC()
	task := domain.Task{
		ID:             idgen.New(),
		WorkspaceID:    workspaceID,
		CreatedBy:      creatorID,
		Name:           req.Name,
		Mode:           req.Mode,
		Status:         domain.TaskPending,
		ConfigSnapshot: raw,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := mysqlstore.InsertTask(ctx, tx, task); err != nil {
		return nil, errcode.New(errcode.UpstreamUnavailable, "写入任务失败")
	}
	for _, ref := range refs {
		if err := mysqlstore.InsertTaskAgent(ctx, tx, task.ID, ref.Ref, ref.AgentID,
			domain.TaskAgentRole(ref.Role)); err != nil {
			return nil, errcode.New(errcode.UpstreamUnavailable, "写入任务成员失败")
		}
	}
	for _, repoID := range req.Repositories {
		if err := mysqlstore.InsertTaskRepository(ctx, tx, task.ID, repoID, "", ""); err != nil {
			return nil, errcode.New(errcode.UpstreamUnavailable, "写入任务仓库失败")
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, errcode.New(errcode.UpstreamUnavailable, "提交事务失败")
	}
	return &task, nil
}

func validateRepositories(ctx context.Context, ex mysqlstore.Executor, workspaceID string, repos []string) error {
	if len(repos) == 0 {
		return nil
	}
	var count int
	err := ex.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM repositories WHERE workspace_id = ? AND id = ?`,
		workspaceID, repos[0]).Scan(&count)
	if err != nil {
		return errcode.New(errcode.UpstreamUnavailable, "校验仓库失败")
	}
	if count == 0 {
		return errcode.Newf(errcode.ValidationFailed, "仓库不存在或不属于该工作区: %s", repos[0])
	}
	return nil
}
