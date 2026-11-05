package domain

import "testing"

func TestWorkItemTransition(t *testing.T) {
	item := &WorkItem{Status: WorkItemProposed}
	steps := []WorkItemEvent{
		WIReviewStarted, WIApprovalRequested, WIApproved,
		WIStarted, WICompleted,
	}
	for _, ev := range steps {
		if err := item.Apply(ev); err != nil {
			t.Fatalf("Apply(%s): %v", ev, err)
		}
	}
	if item.Status != WorkItemDone || !item.Status.IsTerminal() {
		t.Fatalf("最终状态 = %q", item.Status)
	}

	// 非法转换：done 不能再启动
	if err := item.CanTransition(WIStarted); err == nil {
		t.Fatal("done 不应允许 wi.started")
	}
	// 跳级：proposed 不能直接完成
	fresh := &WorkItem{Status: WorkItemProposed}
	if err := fresh.CanTransition(WICompleted); err == nil {
		t.Fatal("proposed 不应允许 wi.completed")
	}
	// 驳回回到 reviewing
	awaiting := &WorkItem{Status: WorkItemAwaitingApproval}
	if err := awaiting.Apply(WIRejected); err != nil || awaiting.Status != WorkItemReviewing {
		t.Fatalf("驳回后 = %q, err=%v", awaiting.Status, err)
	}
}

func TestOnlyOneRunning(t *testing.T) {
	ok := []WorkItem{{Status: WorkItemRunning}, {Status: WorkItemReady}}
	if err := OnlyOneRunning(ok); err != nil {
		t.Fatalf("单个 running 不应报错: %v", err)
	}
	bad := []WorkItem{{Status: WorkItemRunning}, {Status: WorkItemRunning}}
	if err := OnlyOneRunning(bad); err == nil {
		t.Fatal("两个 running 应报错")
	}
}
