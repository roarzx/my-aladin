package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/session"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// SquadDispatch 是派发给小队负责人的一次协作请求。
type SquadDispatch struct {
	TaskID     string
	WorkItemID string
	SessionID  string
	AgentID    string
	MachineID  string
	Prompt     string
}

// SquadDispatcher 把协作请求下发到执行面（测试可替换；空表示只推进状态）。
type SquadDispatcher interface {
	DispatchSquad(ctx context.Context, req SquadDispatch) error
}

// SquadRunner 串行推进小队工作项（F3）。
//
// 与 PlanRunner 共用同一任务锁，防止两种 Runner 同时持锁（F4）。
type SquadRunner struct {
	DB         *sql.DB
	Dispatcher SquadDispatcher
	Sessions   *session.Service
	Lock       *TaskLock
	Owner      string
	Log        *slog.Logger
}

// RunOnce 最多启动一个 ready 工作项；已有 running 时不做任何事。
func (r *SquadRunner) RunOnce(ctx context.Context, taskID string) error {
	if r.Lock != nil {
		release, err := r.Lock.Acquire(ctx, taskID, r.Owner)
		if err != nil {
			if errors.Is(err, ErrTaskLocked) {
				return nil
			}
			return err
		}
		defer release()
	}

	task, err := mysqlstore.GetTask(ctx, r.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return err
	}
	if task.Mode != domain.ModeSquad || task.Status != domain.TaskExecuting {
		return nil
	}

	items, err := mysqlstore.ListWorkItems(ctx, r.DB, taskID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Status == domain.WorkItemRunning {
			return nil
		}
	}

	var next *domain.WorkItem
	for i := range items {
		if items[i].Status == domain.WorkItemReady {
			next = &items[i]
			break
		}
	}
	if next == nil {
		return nil
	}

	started, err := mysqlstore.StartWorkItem(ctx, r.DB, next.ID)
	if errors.Is(err, mysqlstore.ErrWorkItemRunning) {
		return nil
	}
	if err != nil {
		return err
	}

	if err := r.dispatch(ctx, taskID, started); err != nil {
		return err
	}
	_, _ = mysqlstore.AppendActivity(ctx, r.DB, taskID, "work_item.started", map[string]any{
		"work_item_id": started.ID, "ordinal": started.Ordinal,
		"owner_agent_id": started.OwnerAgentID,
	})
	return nil
}

// RetryHandoff 对 blocked 工作项定向重试交接。
func (r *SquadRunner) RetryHandoff(ctx context.Context, workItemID string) error {
	item, err := mysqlstore.GetWorkItem(ctx, r.DB, workItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return errcode.New(errcode.NotFound, "工作项不存在")
	}
	if err != nil {
		return err
	}
	if item.Status != domain.WorkItemBlocked {
		return errcode.Newf(errcode.Conflict, "工作项状态为 %s，不能重试交接", item.Status)
	}
	if err := item.Apply(domain.WIUnblocked); err != nil {
		return errcode.New(errcode.Conflict, err.Error())
	}
	// 重试交接直接从 blocked → running；旧 owner 不变，由负责人重新接收
	version, err := mysqlstore.UpdateWorkItemStatus(ctx, r.DB, item.ID,
		domain.WorkItemBlocked, domain.WorkItemRunning, item.Version-1)
	if err != nil {
		return err
	}
	item.Version = version
	return r.dispatch(ctx, item.TaskID, item)
}

func (r *SquadRunner) dispatch(ctx context.Context, taskID string, item domain.WorkItem) error {
	snapshot, err := mysqlstore.GetSquadSnapshot(ctx, r.DB, taskID)
	if err != nil {
		return r.block(ctx, item, errcode.New(errcode.NotFound, "任务没有冻结的小队快照"))
	}
	cfg, err := mysqlstore.GetAgent(ctx, r.DB, item.OwnerAgentID)
	if err != nil {
		return r.block(ctx, item, errcode.New(errcode.AgentUnavailable, "负责人 Agent 不存在"))
	}

	sessionID := ""
	if r.Sessions != nil {
		record, _, err := r.Sessions.ReuseOrCreate(ctx, taskID, item.OwnerAgentID)
		if err != nil {
			return r.block(ctx, item, err)
		}
		sessionID = record.ID
	}

	if r.Dispatcher == nil {
		return nil
	}
	err = r.Dispatcher.DispatchSquad(ctx, SquadDispatch{
		TaskID: taskID, WorkItemID: item.ID, SessionID: sessionID,
		AgentID: item.OwnerAgentID, MachineID: cfg.MachineID,
		Prompt: snapshot.Instructions,
	})
	if err != nil {
		return r.block(ctx, item, err)
	}
	return nil
}

// block 把 running 工作项置为 blocked，保留失败现场供定向重试。
func (r *SquadRunner) block(ctx context.Context, item domain.WorkItem, cause error) error {
	from := item.Status
	if err := item.Apply(domain.WIBlocked); err == nil {
		if _, err := mysqlstore.UpdateWorkItemStatus(ctx, r.DB, item.ID,
			from, domain.WorkItemBlocked, item.Version-1); err != nil {
			r.log().Warn("标记工作项 blocked 失败", "work_item", item.ID, "err", err)
		}
	}
	_, _ = mysqlstore.AppendActivity(ctx, r.DB, item.TaskID, "work_item.blocked", map[string]any{
		"work_item_id": item.ID, "reason": cause.Error(),
	})
	return cause
}

func (r *SquadRunner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}
