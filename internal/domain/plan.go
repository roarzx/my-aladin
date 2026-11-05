package domain

import (
	"fmt"
	"time"
)

// PlanStatus 对应 plans.status（dev_spec 9.1）。
type PlanStatus string

const (
	PlanIdle      PlanStatus = "idle"
	PlanExecuting PlanStatus = "executing"
	PlanPaused    PlanStatus = "paused"
	PlanStopped   PlanStatus = "stopped"
	PlanCompleted PlanStatus = "completed"
)

var allPlanStatuses = []PlanStatus{
	PlanIdle, PlanExecuting, PlanPaused, PlanStopped, PlanCompleted,
}

// Valid 校验状态取值。
func (s PlanStatus) Valid() error {
	for _, v := range allPlanStatuses {
		if s == v {
			return nil
		}
	}
	return fmt.Errorf("未知的 Plan 状态 %q", s)
}

// IsTerminal 表示计划已结束。
func (s PlanStatus) IsTerminal() bool {
	return s == PlanStopped || s == PlanCompleted
}

// Plan 是某个 revision 的计划快照；版本化、只追加。
type Plan struct {
	ID          string
	TaskID      string
	Revision    int
	ContentHash string
	ContentMD   string
	Status      PlanStatus
	Version     uint64
}

// Confirmation 是对指定计划版本的确认；确认不可移用于新版本。
type Confirmation struct {
	ID          string
	PlanID      string
	Revision    int
	ContentHash string
	ActorID     uint64
	CreatedAt   time.Time
}
