package domain

import "fmt"

// WorkItemStatus 对应 work_items.status（dev_spec 3.3 / 9.1）。
type WorkItemStatus string

const (
	WorkItemProposed         WorkItemStatus = "proposed"
	WorkItemReviewing        WorkItemStatus = "reviewing"
	WorkItemAwaitingApproval WorkItemStatus = "awaiting_approval"
	WorkItemReady            WorkItemStatus = "ready"
	WorkItemRunning          WorkItemStatus = "running"
	WorkItemBlocked          WorkItemStatus = "blocked"
	WorkItemDone             WorkItemStatus = "done"
	WorkItemCancelled        WorkItemStatus = "cancelled"
)

var allWorkItemStatuses = []WorkItemStatus{
	WorkItemProposed, WorkItemReviewing, WorkItemAwaitingApproval, WorkItemReady,
	WorkItemRunning, WorkItemBlocked, WorkItemDone, WorkItemCancelled,
}

// Valid 校验状态取值。
func (s WorkItemStatus) Valid() error {
	for _, v := range allWorkItemStatuses {
		if s == v {
			return nil
		}
	}
	return fmt.Errorf("未知的 WorkItem 状态 %q", s)
}

// IsTerminal 表示工作项已结束。
func (s WorkItemStatus) IsTerminal() bool {
	return s == WorkItemDone || s == WorkItemCancelled
}

// WorkItemEvent 是驱动 WorkItem 状态机的事件。
type WorkItemEvent string

const (
	WIReviewStarted     WorkItemEvent = "wi.review_started"
	WIApprovalRequested WorkItemEvent = "wi.approval_requested"
	WIApproved          WorkItemEvent = "wi.approved"
	WIRejected          WorkItemEvent = "wi.rejected"
	WIStarted           WorkItemEvent = "wi.started"
	WIBlocked           WorkItemEvent = "wi.blocked"
	WIUnblocked         WorkItemEvent = "wi.unblocked"
	WICompleted         WorkItemEvent = "wi.completed"
	WICancelled         WorkItemEvent = "wi.cancelled"
)

var workItemTransitions = map[WorkItemStatus]map[WorkItemEvent]WorkItemStatus{
	WorkItemProposed: {
		WIReviewStarted:     WorkItemReviewing,
		WIApprovalRequested: WorkItemAwaitingApproval,
		WICancelled:         WorkItemCancelled,
	},
	WorkItemReviewing: {
		WIApprovalRequested: WorkItemAwaitingApproval,
		WICancelled:         WorkItemCancelled,
	},
	WorkItemAwaitingApproval: {
		WIApprovalRequested: WorkItemAwaitingApproval, // 修订方案：重新等待审批
		WIApproved:          WorkItemReady,
		WIRejected:          WorkItemReviewing,
		WICancelled:         WorkItemCancelled,
	},
	WorkItemReady: {
		WIApprovalRequested: WorkItemAwaitingApproval, // 已就绪但方案被修订
		WIStarted:           WorkItemRunning,
		WICancelled:         WorkItemCancelled,
	},
	WorkItemRunning: {
		WIBlocked:   WorkItemBlocked,
		WICompleted: WorkItemDone,
		WICancelled: WorkItemCancelled,
	},
	WorkItemBlocked: {
		WIUnblocked: WorkItemRunning,
		WICancelled: WorkItemCancelled,
	},
}

// WorkItem 是小队模式下的工作项。
type WorkItem struct {
	ID           string
	TaskID       string
	Ordinal      int
	OwnerAgentID string
	Status       WorkItemStatus
	Version      uint64
}

// CanTransition 校验事件是否被当前状态接受。
func (w *WorkItem) CanTransition(ev WorkItemEvent) error {
	if _, ok := workItemTransitions[w.Status][ev]; !ok {
		return fmt.Errorf("workitem: 不允许从 %q 经 %q 转换", w.Status, ev)
	}
	return nil
}

// Apply 校验并应用事件；成功后递增 Version。
func (w *WorkItem) Apply(ev WorkItemEvent) error {
	next, ok := workItemTransitions[w.Status][ev]
	if !ok {
		return fmt.Errorf("workitem: 不允许从 %q 经 %q 转换", w.Status, ev)
	}
	w.Status = next
	w.Version++
	return nil
}

// OnlyOneRunning 校验同一任务最多一个 running 工作项（列表需由调用方按 task 过滤）。
func OnlyOneRunning(items []WorkItem) error {
	running := 0
	for _, item := range items {
		if item.Status == WorkItemRunning {
			running++
		}
	}
	if running > 1 {
		return fmt.Errorf("workitem: 同一 Issue 存在 %d 个 running 工作项", running)
	}
	return nil
}
