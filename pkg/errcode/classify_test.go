package errcode_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/roarzx/my-aladin/pkg/errcode"
)

func TestClassify(t *testing.T) {
	toolErr := exec.Command("false").Run()
	if toolErr == nil {
		t.Fatal("false 命令应返回非零退出码")
	}

	cases := []struct {
		name string
		err  error
		want errcode.Class
	}{
		{"超时", context.DeadlineExceeded, errcode.ClassNetwork},
		{"取消", context.Canceled, errcode.ClassNetwork},
		{"机器离线", errcode.New(errcode.MachineOffline, "offline"), errcode.ClassNetwork},
		{"结果未知", errcode.New(errcode.ResultUnknown, "unknown"), errcode.ClassNetwork},
		{"上游不可用", errcode.New(errcode.UpstreamUnavailable, "down"), errcode.ClassNetwork},
		{"运行时起不来", errcode.New(errcode.RuntimeStartFailed, "boom"), errcode.ClassRuntime},
		{"权限错误码", errcode.New(errcode.Forbidden, "no"), errcode.ClassPermission},
		{"未认证", errcode.New(errcode.Unauthenticated, "no"), errcode.ClassPermission},
		{"文件权限", os.ErrPermission, errcode.ClassPermission},
		{"工具退出码", toolErr, errcode.ClassTool},
		{"验证不完整", errcode.New(errcode.VerificationIncomplete, "test failed"), errcode.ClassVerification},
		{"未知错误", errors.New("boom"), errcode.ClassRuntime},
		{"包装的错误码", fmtWrap{errcode.New(errcode.Forbidden, "wrapped")}, errcode.ClassPermission},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errcode.Classify(tc.err); got != tc.want {
				t.Fatalf("Classify(%v) = %q, 期望 %q", tc.err, got, tc.want)
			}
		})
	}
}

// fmtWrap 模拟 %w 包装后的错误链。
type fmtWrap struct{ err error }

func (w fmtWrap) Error() string { return "wrap: " + w.err.Error() }
func (w fmtWrap) Unwrap() error { return w.err }

func TestPolicy(t *testing.T) {
	if p := errcode.Policy(errcode.ClassPermission); p.Auto || p.Manual {
		t.Fatalf("permission 不应可重试: %+v", p)
	}
	if p := errcode.Policy(errcode.ClassVerification); p.Auto || !p.Manual {
		t.Fatalf("verification 应只允许人工重试: %+v", p)
	}
	if p := errcode.Policy(errcode.ClassNetwork); !p.Auto || p.Budget != -1 {
		t.Fatalf("network 应在预算内自动重试: %+v", p)
	}
	if p := errcode.Policy(errcode.ClassRuntime); !p.Auto || p.Budget != 1 {
		t.Fatalf("runtime 应自动重试 1 次: %+v", p)
	}
	if p := errcode.Policy(errcode.ClassTool); !p.Auto || p.Budget != 1 {
		t.Fatalf("tool 应自动重试 1 次: %+v", p)
	}
}
