package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/roarzx/my-aladin/internal/domain"
)

func TestPolicyMatrix(t *testing.T) {
	const me = uint64(7)

	type tc struct {
		name      string
		role      domain.Role
		action    Action
		taskOwner uint64
		want      bool
	}

	cases := []tc{
		// 查看：所有角色
		{"owner view", domain.RoleOwner, ActionView, 0, true},
		{"admin view", domain.RoleAdmin, ActionView, 0, true},
		{"member view", domain.RoleMember, ActionView, 0, true},
		{"viewer view", domain.RoleViewer, ActionView, 0, true},

		// 创建 Issue：viewer 不行
		{"owner create", domain.RoleOwner, ActionCreateIssue, 0, true},
		{"admin create", domain.RoleAdmin, ActionCreateIssue, 0, true},
		{"member create", domain.RoleMember, ActionCreateIssue, 0, true},
		{"viewer create", domain.RoleViewer, ActionCreateIssue, 0, false},

		// 确认启动 / 重试 / 取消：member 仅限自己创建的
		{"owner confirm", domain.RoleOwner, ActionConfirmStart, 0, true},
		{"admin retry", domain.RoleAdmin, ActionRetryStep, 0, true},
		{"member cancel own", domain.RoleMember, ActionCancelTask, me, true},
		{"member confirm other", domain.RoleMember, ActionConfirmStart, me + 1, false},
		{"member retry unknown owner", domain.RoleMember, ActionRetryStep, 0, false},
		{"viewer confirm", domain.RoleViewer, ActionConfirmStart, me, false},

		// 审批：viewer 不行（具体顺序由 F 阶段负责）
		{"owner approve", domain.RoleOwner, ActionApprove, 0, true},
		{"admin approve", domain.RoleAdmin, ActionApprove, 0, true},
		{"member approve", domain.RoleMember, ActionApprove, 0, true},
		{"viewer approve", domain.RoleViewer, ActionApprove, 0, false},

		// 管理成员 / 能力
		{"owner manage members", domain.RoleOwner, ActionManageMembers, 0, true},
		{"admin manage members", domain.RoleAdmin, ActionManageMembers, 0, true},
		{"member manage members", domain.RoleMember, ActionManageMembers, 0, false},
		{"viewer manage caps", domain.RoleViewer, ActionManageCapabilities, 0, false},
		{"admin manage caps", domain.RoleAdmin, ActionManageCapabilities, 0, true},

		// 未知动作
		{"unknown action", domain.RoleOwner, Action("nuke"), 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actor := Actor{UserID: me, Role: tc.role, WorkspaceID: "ws-1"}
			got := Can(actor, tc.action, Resource{WorkspaceID: "ws-1", TaskOwnerID: tc.taskOwner})
			if got != tc.want {
				t.Fatalf("Can(%s, %s, owner=%d) = %v, 期望 %v",
					tc.role, tc.action, tc.taskOwner, got, tc.want)
			}
		})
	}
}

func TestPolicyWorkspaceIsolation(t *testing.T) {
	actor := Actor{UserID: 1, Role: domain.RoleOwner, WorkspaceID: "ws-1"}
	if Can(actor, ActionView, Resource{WorkspaceID: "ws-2"}) {
		t.Fatal("跨工作区必须拒绝")
	}
	if !Can(actor, ActionView, Resource{WorkspaceID: "ws-1"}) {
		t.Fatal("同工作区应通过")
	}
}

func TestPolicyInvalidRoleFailsClosed(t *testing.T) {
	actor := Actor{UserID: 1, Role: domain.Role("root")}
	if Can(actor, ActionView, Resource{}) {
		t.Fatal("非法角色必须拒绝")
	}
}

func TestRequireRoleMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("未认证", func(t *testing.T) {
		rec := httptest.NewRecorder()
		RequireRole(domain.RoleMember)(okHandler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("状态 = %d", rec.Code)
		}
	})

	t.Run("viewer 不足", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(WithActor(req.Context(), Actor{UserID: 1, Role: domain.RoleViewer}))
		rec := httptest.NewRecorder()
		RequireRole(domain.RoleMember)(okHandler).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("状态 = %d", rec.Code)
		}
	})

	t.Run("admin 通过", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(WithActor(req.Context(), Actor{UserID: 1, Role: domain.RoleAdmin}))
		rec := httptest.NewRecorder()
		RequireRole(domain.RoleMember)(okHandler).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("状态 = %d", rec.Code)
		}
	})
}
