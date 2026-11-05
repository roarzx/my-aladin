package health

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubDep struct {
	name string
	err  error
}

func (s stubDep) Name() string                { return s.name }
func (s stubDep) Check(context.Context) error { return s.err }

func TestCheckerHealthy(t *testing.T) {
	c := New(stubDep{name: "mysql"}, stubDep{name: "redis"})
	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("期望健康，实际: %v", err)
	}
	if d := c.Details(context.Background()); len(d) != 0 {
		t.Fatalf("期望空详情，实际: %v", d)
	}
}

func TestCheckerReportsAllFailures(t *testing.T) {
	c := New(
		stubDep{name: "mysql", err: errors.New("connection refused")},
		stubDep{name: "redis", err: errors.New("timeout")},
		stubDep{name: "ok"},
	)
	err := c.Check(context.Background())
	if err == nil {
		t.Fatal("期望失败")
	}
	for _, want := range []string{"mysql", "redis", "connection refused", "timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息缺少 %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "ok:") {
		t.Errorf("健康依赖不应出现在错误里: %v", err)
	}

	details := c.Details(context.Background())
	if len(details) != 2 || details["mysql"] == "" || details["redis"] == "" {
		t.Fatalf("详情不正确: %v", details)
	}
}
