package auth

import (
	"net/http"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Middleware 是标准的 HTTP 中间件形态。
type Middleware func(http.Handler) http.Handler

// RequireRole 要求当前主体角色不低于 min。
func RequireRole(min domain.Role) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, ok := ActorFrom(r.Context())
			if !ok {
				errcode.Write(w, errcode.New(errcode.Unauthenticated, "未认证"))
				return
			}
			if !actor.Role.AtLeast(min) {
				errcode.Write(w, errcode.New(errcode.Forbidden, "角色权限不足"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAction 按权限矩阵校验；resource 负责从请求计算资源信息。
func RequireAction(action Action, resource func(*http.Request) Resource) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, ok := ActorFrom(r.Context())
			if !ok {
				errcode.Write(w, errcode.New(errcode.Unauthenticated, "未认证"))
				return
			}
			res := Resource{}
			if resource != nil {
				res = resource(r)
			}
			if !Can(actor, action, res) {
				errcode.Write(w, errcode.New(errcode.Forbidden, "无权限执行该操作"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
