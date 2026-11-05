// Package squad 实现小队快照、方案与审批链（F1/F5）。
package squad

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Service 管理小队定义与冻结。
type Service struct {
	DB *sql.DB
}

// Freeze 把小队定义冻结到任务；同一任务重复冻结返回已有快照（幂等）。
func (s *Service) Freeze(ctx context.Context, squadID, taskID string) (domain.SquadSnapshot, error) {
	return FreezeTx(ctx, s.DB, squadID, taskID)
}

// FreezeTx 在事务内冻结小队。
func FreezeTx(ctx context.Context, ex mysqlstore.Executor, squadID, taskID string) (domain.SquadSnapshot, error) {
	existing, err := mysqlstore.GetSquadSnapshot(ctx, ex, taskID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.SquadSnapshot{}, err
	}

	def, err := mysqlstore.GetSquad(ctx, ex, squadID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.SquadSnapshot{}, errcode.New(errcode.NotFound, "小队不存在")
	}
	if err != nil {
		return domain.SquadSnapshot{}, errcode.New(errcode.UpstreamUnavailable, "读取小队失败")
	}
	if !def.Enabled {
		return domain.SquadSnapshot{}, errcode.New(errcode.ValidationFailed, "小队已停用")
	}
	if err := def.ApprovalPolicy.Valid(); err != nil {
		return domain.SquadSnapshot{}, errcode.Newf(errcode.ValidationFailed, "审批配置不合法: %v", err)
	}

	snap := domain.SquadSnapshot{
		ID: idgen.New(), TaskID: taskID, SquadID: def.ID,
		LeaderAgentID: def.LeaderAgentID, MemberIDs: def.MemberIDs,
		Instructions: def.Instructions, ApprovalPolicy: def.ApprovalPolicy,
		FrozenAt: time.Now().UTC(),
	}
	if err := mysqlstore.InsertSquadSnapshot(ctx, ex, snap); err != nil {
		// 并发创建：读取已冻结快照
		if existing, getErr := mysqlstore.GetSquadSnapshot(ctx, ex, taskID); getErr == nil {
			return existing, nil
		}
		return domain.SquadSnapshot{}, errcode.New(errcode.UpstreamUnavailable, "冻结小队失败")
	}
	return snap, nil
}

// SnapshotForTask 读取任务的冻结小队。
func SnapshotForTask(ctx context.Context, ex mysqlstore.Executor, taskID string) (domain.SquadSnapshot, error) {
	return mysqlstore.GetSquadSnapshot(ctx, ex, taskID)
}
