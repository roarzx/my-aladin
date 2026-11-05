package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
)

// ErrLeaseLost 表示租约不存在、已过期或 epoch 不再是持有者。
var ErrLeaseLost = errors.New("lease: 租约已失效")

// LeaseStore 是租约的 Redis 侧能力（redisstore.LeaseStore 实现）。
type LeaseStore interface {
	Acquire(ctx context.Context, attemptID string, epoch uint64, ttl time.Duration) (bool, error)
	Renew(ctx context.Context, attemptID string, epoch uint64, ttl time.Duration) (bool, error)
	Exists(ctx context.Context, attemptID string) (bool, error)
	Delete(ctx context.Context, attemptID string) (bool, error)
}

// Lease 管理 Step 级执行租约（E4）。
type Lease struct {
	DB    *sql.DB
	Store LeaseStore
	TTL   time.Duration
	Log   *slog.Logger
}

func (l *Lease) ttl() time.Duration {
	if l.TTL <= 0 {
		return 60 * time.Second
	}
	return l.TTL
}

// Acquire 在派发新尝试时建立租约。
func (l *Lease) Acquire(ctx context.Context, attempt domain.Attempt) error {
	ok, err := l.Store.Acquire(ctx, attempt.ID, attempt.LeaseEpoch, l.ttl())
	if err != nil {
		return err
	}
	if !ok {
		return ErrLeaseLost
	}
	return nil
}

// Renew 续约；旧 epoch 的续约被拒绝。
func (l *Lease) Renew(ctx context.Context, attemptID string, epoch uint64) error {
	attempt, err := mysqlstore.GetAttemptByID(ctx, l.DB, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLeaseLost
	}
	if err != nil {
		return err
	}
	if attempt.LeaseEpoch != epoch {
		return ErrLeaseLost
	}
	ok, err := l.Store.Renew(ctx, attemptID, epoch, l.ttl())
	if err != nil {
		return err
	}
	if !ok {
		return ErrLeaseLost
	}
	return nil
}

// RenewByExecution 按 execution_id 续约（心跳路径）。
func (l *Lease) RenewByExecution(ctx context.Context, executionID string, epoch uint64) error {
	attempt, err := mysqlstore.GetAttemptByExecution(ctx, l.DB, executionID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLeaseLost
	}
	if err != nil {
		return err
	}
	return l.Renew(ctx, attempt.ID, epoch)
}

// Released 是过期租约对应的尝试。
type Expired struct {
	AttemptID string
	StepID    string
}

// Expire 扫描活跃尝试：租约消失即 attempt→unknown、Step→reconciling（不直接判 failed）。
func (l *Lease) Expire(ctx context.Context) ([]Expired, error) {
	attempts, err := mysqlstore.ListActiveAttempts(ctx, l.DB, 200)
	if err != nil {
		return nil, err
	}

	var expired []Expired
	for _, attempt := range attempts {
		exists, err := l.Store.Exists(ctx, attempt.ID)
		if err != nil {
			return expired, err
		}
		if exists {
			continue
		}

		step, err := mysqlstore.GetStepByID(ctx, l.DB, attempt.StepID)
		if err != nil {
			l.log().Warn("租约过期但读取步骤失败", "attempt", attempt.ID, "err", err)
			continue
		}

		if err := mysqlstore.UpdateAttemptStatus(ctx, l.DB, attempt.ID,
			domain.AttemptUnknown, "", "", ""); err != nil {
			l.log().Warn("标记结果未知失败", "attempt", attempt.ID, "err", err)
			continue
		}
		if step.Status == domain.StepExecuting {
			if _, err := mysqlstore.UpdateStepState(ctx, l.DB, step.ID,
				domain.StepExecuting, domain.StepReconciling, step.Version); err != nil {
				l.log().Warn("步骤转对账失败", "step", step.ID, "err", err)
			}
		}
		if plan, _, err := mysqlstore.GetPlanByID(ctx, l.DB, step.PlanID); err == nil {
			_, _ = mysqlstore.AppendActivity(ctx, l.DB, plan.TaskID, "step.reconciling", map[string]any{
				"step_id": step.ID, "attempt_id": attempt.ID, "reason": "lease_expired",
			})
			// 9.8：租约超时 → Plan/Task paused（等待对账，而不是 failed）
			if plan.Status == domain.PlanExecuting {
				if _, err := mysqlstore.UpdatePlanState(ctx, l.DB, plan.ID,
					domain.PlanExecuting, domain.PlanPaused, plan.Version); err == nil {
					if task, err := mysqlstore.GetTask(ctx, l.DB, plan.TaskID); err == nil &&
						task.Status == domain.TaskExecuting {
						_, _ = mysqlstore.UpdateTaskState(ctx, l.DB, plan.TaskID,
							domain.TaskExecuting, domain.TaskPaused, task.Version)
					}
				}
			}
		}
		expired = append(expired, Expired{AttemptID: attempt.ID, StepID: step.ID})
	}
	return expired, nil
}

func (l *Lease) log() *slog.Logger {
	if l.Log != nil {
		return l.Log
	}
	return slog.Default()
}
