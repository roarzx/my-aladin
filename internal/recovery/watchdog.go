// Package recovery 提供 Watchdog 与对账流程（E5/E6）。
package recovery

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/orchestration"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
)

// Runner 是 Watchdog 推进任务的最小能力（PlanRunner 满足）。
type Runner interface {
	RunOnce(ctx context.Context, taskID string) error
}

// Watchdog 按 9.8 的周期扫描执行中任务与过期租约（E5）。
type Watchdog struct {
	DB            *sql.DB
	Runner        Runner
	Lease         *orchestration.Lease
	Canceller     *orchestration.Canceller
	CancelTimeout time.Duration
	Interval      time.Duration
	Log           *slog.Logger
}

// SweepResult 是一轮扫描的摘要。
type SweepResult struct {
	Tasks     int   `json:"tasks"`
	Advanced  int   `json:"advanced"`
	Expired   int   `json:"expired"`
	Cancelled int   `json:"cancelled"`
	Errors    int   `json:"errors"`
	LastError error `json:"-"`
}

// Sweep 扫描一轮：重新接管无锁的执行中任务，并把过期租约转入对账。
func (w *Watchdog) Sweep(ctx context.Context) (SweepResult, error) {
	var result SweepResult

	taskIDs, err := mysqlstore.ListTaskIDsByStatus(ctx, w.DB, domain.TaskExecuting, 200)
	if err != nil {
		return result, err
	}
	result.Tasks = len(taskIDs)

	if w.Runner != nil {
		for _, taskID := range taskIDs {
			if err := w.Runner.RunOnce(ctx, taskID); err != nil {
				result.Errors++
				result.LastError = err
				w.log().Warn("Watchdog 推进任务失败", "task", taskID, "err", err)
				continue
			}
			result.Advanced++
		}
	}

	if w.Canceller != nil && w.CancelTimeout > 0 {
		ids, err := mysqlstore.ListTaskIDsByStatus(ctx, w.DB, domain.TaskCancelling, 200)
		if err != nil {
			result.Errors++
			result.LastError = err
		}
		for _, taskID := range ids {
			task, err := mysqlstore.GetTask(ctx, w.DB, taskID)
			if err != nil || task.CancelRequestedAt == nil {
				continue
			}
			if time.Since(*task.CancelRequestedAt) < w.CancelTimeout {
				continue
			}
			if err := w.Canceller.FinalizeCancel(ctx, taskID, true); err != nil {
				result.Errors++
				result.LastError = err
				continue
			}
			result.Cancelled++
		}
	}

	if w.Lease != nil {
		expired, err := w.Lease.Expire(ctx)
		if err != nil {
			result.Errors++
			result.LastError = err
			w.log().Warn("租约扫描失败", "err", err)
		} else {
			result.Expired = len(expired)
		}
	}
	return result, result.LastError
}

// Run 以固定周期执行 Sweep，直到 ctx 取消。
func (w *Watchdog) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = w.Sweep(ctx)
		}
	}
}

func (w *Watchdog) log() *slog.Logger {
	if w.Log != nil {
		return w.Log
	}
	return slog.Default()
}
