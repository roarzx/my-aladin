package domain

import "testing"

func TestParseReportedStatus(t *testing.T) {
	cases := map[string]TaskStatus{
		"in_progress": TaskExecuting,
		"executing":   TaskExecuting,
		"blocked":     TaskPaused,
		"paused":      TaskPaused,
		"completed":   TaskCompleted,
		"done":        TaskCompleted,
		"canceled":    TaskCancelled,
		"failed":      TaskFailed,
		" Pending ":   TaskPending,
	}
	for raw, want := range cases {
		got, err := ParseReportedStatus(raw)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if got != want {
			t.Fatalf("%q = %q，期望 %q", raw, got, want)
		}
	}
	if _, err := ParseReportedStatus("flying"); err == nil {
		t.Fatal("非法状态应报错")
	}
	if _, err := ParseReportedStatus("  "); err == nil {
		t.Fatal("空状态应报错")
	}
}

func TestTaskReportStatus(t *testing.T) {
	task := &Task{Status: TaskExecuting, Version: 3}
	if err := task.ReportStatus(TaskCompleted); err != nil {
		t.Fatal(err)
	}
	if task.Status != TaskCompleted || task.Version != 4 {
		t.Fatalf("上报后 status=%q version=%d", task.Status, task.Version)
	}

	// 幂等：重复上报同一状态不递增版本。
	if err := task.ReportStatus(TaskCompleted); err != nil {
		t.Fatal(err)
	}
	if task.Version != 4 {
		t.Fatalf("重复上报不应递增版本: %d", task.Version)
	}

	// 完成后可以回到进行中（用户追问继续干）。
	if err := task.ReportStatus(TaskExecuting); err != nil {
		t.Fatal(err)
	}

	// 取消/失败是终态。
	task.Status = TaskCancelled
	if err := task.ReportStatus(TaskExecuting); err == nil {
		t.Fatal("终态不应被复活")
	}
	if err := task.ReportStatus(TaskFailed); err == nil {
		t.Fatal("终态之间不应互相切换")
	}
	if err := task.ReportStatus(TaskCancelled); err != nil {
		t.Fatalf("重复上报终态应幂等: %v", err)
	}
}
