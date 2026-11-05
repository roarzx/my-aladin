package errcode

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"syscall"
)

// Class 是失败分类（dev_spec 3.4 重试策略表）。
type Class string

const (
	ClassNetwork      Class = "network"
	ClassRuntime      Class = "runtime"
	ClassTool         Class = "tool"
	ClassVerification Class = "verification"
	ClassPermission   Class = "permission"
)

var allClasses = []Class{
	ClassNetwork, ClassRuntime, ClassTool, ClassVerification, ClassPermission,
}

// Valid 校验分类取值。
func (c Class) Valid() error {
	for _, v := range allClasses {
		if c == v {
			return nil
		}
	}
	return fmt.Errorf("未知的错误分类 %q", c)
}

func (c Class) String() string { return string(c) }

// RetryPolicy 是某个分类的处置策略。
type RetryPolicy struct {
	// Auto 表示可自动重试（无需人工新指令）。
	Auto bool
	// Budget 是自动重试次数上限；-1 表示不单独设限（受整体重试预算约束）。
	Budget int
	// Manual 表示允许人工重试（需提供新指令）。
	Manual bool
}

// Policy 返回 dev_spec 3.4 重试策略表对应的处置。
func Policy(c Class) RetryPolicy {
	switch c {
	case ClassNetwork:
		return RetryPolicy{Auto: true, Budget: -1, Manual: true}
	case ClassRuntime:
		return RetryPolicy{Auto: true, Budget: 1, Manual: true}
	case ClassTool:
		return RetryPolicy{Auto: true, Budget: 1, Manual: true}
	case ClassVerification:
		return RetryPolicy{Auto: false, Budget: 0, Manual: true}
	case ClassPermission:
		return RetryPolicy{Auto: false, Budget: 0, Manual: false}
	default:
		return RetryPolicy{Auto: false, Budget: 0, Manual: false}
	}
}

// Classify 在失败发生时刻给出分类，禁止事后推测。
//
// 分类依据：错误码优先，其次错误值（超时/网络/权限/工具退出）。
func Classify(err error) Class {
	if err == nil {
		return ClassRuntime
	}

	var coded *Error
	if errors.As(err, &coded) {
		switch coded.Code {
		case Forbidden, Unauthenticated:
			return ClassPermission
		case VerificationIncomplete:
			return ClassVerification
		case MachineOffline, UpstreamUnavailable, ResultUnknown:
			return ClassNetwork
		case AgentUnavailable:
			// Agent 被移除/白名单变更：修复动作在权限侧，重试无意义
			return ClassPermission
		case RuntimeStartFailed, InternalError:
			return ClassRuntime
		}
	}

	if errors.Is(err, os.ErrPermission) || errors.Is(err, fs.ErrPermission) ||
		errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return ClassPermission
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ClassNetwork
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return ClassNetwork
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return ClassTool
	}
	return ClassRuntime
}
