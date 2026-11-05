package session_test

import (
	"testing"

	"github.com/roarzx/my-aladin/internal/session"
)

type fakeSession struct{ fp string }

func (f fakeSession) SessionFingerprint() string { return f.fp }

func TestFingerprint(t *testing.T) {
	base := session.Fingerprint(`{"servers":[]}`, []string{"A=1", "B=2"}, "prompt")

	if got := session.Fingerprint(`{"servers":[]}`, []string{"B=2", "A=1"}, "prompt"); got != base {
		t.Fatal("环境变量顺序不应影响指纹")
	}
	if got := session.Fingerprint(`{"servers":[{"name":"x"}]}`, []string{"A=1", "B=2"}, "prompt"); got == base {
		t.Fatal("MCP 配置变化必须改变指纹")
	}
	if got := session.Fingerprint(`{"servers":[]}`, []string{"A=1"}, "prompt"); got == base {
		t.Fatal("环境变化必须改变指纹")
	}
	if got := session.Fingerprint(`{"servers":[]}`, []string{"A=1", "B=2"}, "prompt2"); got == base {
		t.Fatal("prompt 变化必须改变指纹")
	}
}

func TestCanResume(t *testing.T) {
	fp := session.Fingerprint("mcp", []string{"A=1"}, "p")
	if !session.CanResume(fakeSession{fp: fp}, fp) {
		t.Fatal("三项全等应允许复用")
	}
	if session.CanResume(fakeSession{fp: fp}, "other") {
		t.Fatal("指纹不同不应复用")
	}
	if session.CanResume(fakeSession{fp: fp}, "") {
		t.Fatal("空指纹不应复用")
	}
	if session.CanResume(nil, fp) {
		t.Fatal("无会话记录不应复用")
	}
}
