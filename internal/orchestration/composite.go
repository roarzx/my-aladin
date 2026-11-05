package orchestration

import (
	"context"
	"database/sql"
	"errors"

	"github.com/roarzx/my-aladin/internal/domain"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// CompositeRunner 按任务模式选择 PlanRunner 或 SquadRunner（F4）。
type CompositeRunner struct {
	DB    *sql.DB
	Plan  Runner
	Squad Runner
}

// Runner 是 RunOnce 的最小接口。
type Runner interface {
	RunOnce(ctx context.Context, taskID string) error
}

// RunOnce 读取任务模式并委派；两种 Runner 共用同一任务锁键。
func (r *CompositeRunner) RunOnce(ctx context.Context, taskID string) error {
	task, err := mysqlstore.GetTask(ctx, r.DB, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return errcode.New(errcode.NotFound, "任务不存在")
	}
	if err != nil {
		return err
	}
	if task.Mode == domain.ModeSquad {
		if r.Squad == nil {
			return nil
		}
		return r.Squad.RunOnce(ctx, taskID)
	}
	if r.Plan == nil {
		return nil
	}
	return r.Plan.RunOnce(ctx, taskID)
}
