package auth

import "github.com/roarzx/my-aladin/internal/domain"

// Action 是需要授权的操作（dev_spec 9.5 的行）。
type Action string

const (
	ActionView               Action = "view"
	ActionCreateIssue        Action = "create_issue"
	ActionConfirmStart       Action = "confirm_start"
	ActionRetryStep          Action = "retry_step"
	ActionCancelTask         Action = "cancel_task"
	ActionApprove            Action = "approve"
	ActionManageMembers      Action = "manage_members"
	ActionManageCapabilities Action = "manage_capabilities"
)

// Resource 是授权判定所需的资源信息。
type Resource struct {
	WorkspaceID string
	// TaskOwnerID 是任务创建者；0 表示未知（fail-closed）。
	TaskOwnerID uint64
}

// Can 是权限矩阵的纯函数实现（dev_spec 9.5）。
//
// "审批方案" 在这里只校验角色；具体审批链顺序由 F 阶段的审批逻辑负责。
func Can(actor Actor, action Action, res Resource) bool {
	if actor.Role.Valid() != nil {
		return false
	}
	if res.WorkspaceID != "" && actor.WorkspaceID != "" &&
		res.WorkspaceID != actor.WorkspaceID {
		return false
	}

	role := actor.Role
	switch action {
	case ActionView:
		return true
	case ActionCreateIssue:
		return role != domain.RoleViewer
	case ActionConfirmStart, ActionRetryStep, ActionCancelTask:
		switch role {
		case domain.RoleOwner, domain.RoleAdmin:
			return true
		case domain.RoleMember:
			return res.TaskOwnerID != 0 && res.TaskOwnerID == actor.UserID
		default:
			return false
		}
	case ActionApprove:
		return role != domain.RoleViewer
	case ActionManageMembers, ActionManageCapabilities:
		return role == domain.RoleOwner || role == domain.RoleAdmin
	default:
		return false
	}
}
