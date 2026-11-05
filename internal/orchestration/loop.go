package orchestration

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
)

// Loop 周期性推进所有 executing 任务（D8 的驱动者）。
type Loop struct {
	DB       *sql.DB
	Runner   *PlanRunner
	Interval time.Duration
	Log      *slog.Logger
}

// Run 启动循环，直到 ctx 取消。
func (l *Loop) Run(ctx context.Context) {
	interval := l.Interval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.Sweep(ctx)
		}
	}
}

// Sweep 扫描一次 executing 任务并调用 Runner。
func (l *Loop) Sweep(ctx context.Context) {
	if l.Runner == nil {
		return
	}
	ids, err := mysqlstore.ListTaskIDsByStatus(ctx, l.DB, domain.TaskExecuting, 100)
	if err != nil {
		l.log().Warn("扫描 executing 任务失败", "err", err)
		return
	}
	for _, taskID := range ids {
		if err := l.Runner.RunOnce(ctx, taskID); err != nil {
			l.log().Warn("推进任务失败", "task", taskID, "err", err)
		}
	}
}

func (l *Loop) log() *slog.Logger {
	if l.Log != nil {
		return l.Log
	}
	return slog.Default()
}
