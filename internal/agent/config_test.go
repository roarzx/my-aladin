package agent

import (
	"testing"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

func TestVisibility(t *testing.T) {
	for _, v := range []Visibility{Private, Shared} {
		if err := v.Valid(); err != nil {
			t.Errorf("%q 应为合法可见性", v)
		}
	}
	if err := Visibility("public").Valid(); err == nil {
		t.Error("未知可见性应被拒绝")
	}
}

func TestVisibleTo(t *testing.T) {
	shared := Agent{Visibility: Shared, OwnerID: 1}
	if !shared.VisibleTo(2) {
		t.Error("shared 应对所有成员可见")
	}

	private := Agent{Visibility: Private, OwnerID: 1}
	if !private.VisibleTo(1) {
		t.Error("private 应对创建者可见")
	}
	if private.VisibleTo(2) {
		t.Error("private 不应对其他用户可见")
	}

	broken := Agent{Visibility: "weird", OwnerID: 1}
	if broken.VisibleTo(1) {
		t.Error("非法可见性必须 fail-closed")
	}
}

func TestSchedulableBy(t *testing.T) {
	cases := []struct {
		name    string
		agent   Agent
		user    uint64
		wantErr bool
	}{
		{"shared ok", Agent{Visibility: Shared, Enabled: true}, 2, false},
		{"private owner ok", Agent{Visibility: Private, OwnerID: 1, Enabled: true}, 1, false},
		{"private other denied", Agent{Visibility: Private, OwnerID: 1, Enabled: true}, 2, true},
		{"disabled denied", Agent{Visibility: Shared, Enabled: false}, 1, true},
		{"invalid visibility denied", Agent{Visibility: "weird", Enabled: true}, 1, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.agent.SchedulableBy(tc.user)
			if tc.wantErr {
				if err == nil {
					t.Fatal("期望拒绝")
				}
				e, ok := errcode.As(err)
				if !ok || e.Code != errcode.AgentUnavailable {
					t.Fatalf("错误码应为 AGENT_UNAVAILABLE，实际: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望允许: %v", err)
			}
		})
	}
}

func TestTaskRole(t *testing.T) {
	a := Agent{ID: "agent-1"}
	if got := a.TaskRole("agent-1"); got != domain.TaskAgentPlanner {
		t.Errorf("角色 = %q", got)
	}
	if got := a.TaskRole("agent-2"); got != domain.TaskAgentMember {
		t.Errorf("角色 = %q", got)
	}
}
