// Package session 负责会话创建、规划派发与回复生命周期（D1/D4）。
package session

import (
	"context"
	"database/sql"
	"errors"

	"github.com/roarzx/my-aladin/internal/machine"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// PlanningDispatch 是下发给 daemon 的规划请求。
type PlanningDispatch struct {
	TaskID    string
	SessionID string
	AgentID   string
	MachineID string
	Prompt    string
}

// Dispatcher 把规划会话下发到执行面（真实实现走 WS，测试用假实现）。
type Dispatcher interface {
	DispatchPlanning(ctx context.Context, req PlanningDispatch) error
}

// Service 管理会话状态。
type Service struct {
	DB         *sql.DB
	Dispatcher Dispatcher
}

// Ensure 返回或创建指定 kind 的会话。
func (s *Service) Ensure(ctx context.Context, taskID, agentID, kind string) (mysqlstore.SessionRecord, error) {
	return mysqlstore.EnsureSession(ctx, s.DB, taskID, agentID, kind)
}

// DispatchPlanning 为任务创建规划会话并派发。
//
// 机器离线时只把会话置为 waiting_agent 并返回 nil（不报错）；
// 派发被拒时置为 dispatch_failed。没有接入 Dispatcher 时保持 waiting_agent，绝不假装已派发。
func (s *Service) DispatchPlanning(ctx context.Context, taskID, prompt string) error {
	agentID, err := mysqlstore.GetPlannerAgentID(ctx, s.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return errcode.New(errcode.NotFound, "任务没有 Planner")
	}
	if err != nil {
		return errcode.New(errcode.UpstreamUnavailable, "读取 Planner 失败")
	}

	record, err := mysqlstore.EnsureSession(ctx, s.DB, taskID, agentID, "planning")
	if err != nil {
		return errcode.New(errcode.UpstreamUnavailable, "创建会话失败")
	}

	agentConfig, err := mysqlstore.GetAgent(ctx, s.DB, agentID)
	if err != nil {
		return errcode.New(errcode.AgentUnavailable, "Agent 配置不存在")
	}

	online := false
	if m, err := mysqlstore.GetMachine(ctx, s.DB, agentConfig.MachineID); err == nil {
		online = m.Status == machine.StatusOnline
	} else if !errors.Is(err, sql.ErrNoRows) {
		return errcode.New(errcode.UpstreamUnavailable, "读取机器状态失败")
	}

	if s.Dispatcher == nil {
		return mysqlstore.SetSessionDispatch(ctx, s.DB, record.ID, "waiting_agent", agentConfig.MachineID)
	}
	if !online {
		return mysqlstore.SetSessionDispatch(ctx, s.DB, record.ID, "waiting_agent", agentConfig.MachineID)
	}

	err = s.Dispatcher.DispatchPlanning(ctx, PlanningDispatch{
		TaskID: taskID, SessionID: record.ID, AgentID: agentID,
		MachineID: agentConfig.MachineID, Prompt: prompt,
	})
	if err != nil {
		_ = mysqlstore.SetSessionDispatch(ctx, s.DB, record.ID, "dispatch_failed", agentConfig.MachineID)
		return nil
	}
	return mysqlstore.SetSessionDispatch(ctx, s.DB, record.ID, "dispatched", agentConfig.MachineID)
}
