package domain

import "fmt"

// Role 是工作区成员角色（dev_spec 9.5）。
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

// Valid 校验角色取值。
func (r Role) Valid() error {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember, RoleViewer:
		return nil
	}
	return fmt.Errorf("未知的工作区角色 %q", r)
}

func (r Role) rank() int {
	switch r {
	case RoleOwner:
		return 3
	case RoleAdmin:
		return 2
	case RoleMember:
		return 1
	case RoleViewer:
		return 0
	default:
		return -1
	}
}

// AtLeast 表示角色权限不低于 min；任一角色非法时返回 false（fail-closed）。
func (r Role) AtLeast(min Role) bool {
	if r.Valid() != nil || min.Valid() != nil {
		return false
	}
	return r.rank() >= min.rank()
}
