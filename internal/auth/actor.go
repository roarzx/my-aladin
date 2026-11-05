// Package auth 负责身份提取与权限判定（dev_spec 9.5 / B2）。
package auth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Actor 是已认证的请求主体。
type Actor struct {
	UserID      uint64
	WorkspaceID string
	Role        domain.Role
}

type ctxKey int

const actorKey ctxKey = iota

// WithActor 把主体放入 context。
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey, a)
}

// ActorFrom 取出请求主体。
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey).(Actor)
	return a, ok
}

// Authenticator 从请求中解析主体。
type Authenticator interface {
	Authenticate(r *http.Request) (Actor, error)
}

// StaticToken 是 v1 的最小认证方案：一个工作区管理令牌对应一个用户。
// 令牌只从环境变量 ALADDIN_ADMIN_TOKEN 读取。
type StaticToken struct {
	Token  string
	UserID uint64
}

const bearerPrefix = "Bearer "

// Authenticate 校验 Authorization: Bearer <token>。
func (s StaticToken) Authenticate(r *http.Request) (Actor, error) {
	if s.Token == "" {
		return Actor{}, errcode.New(errcode.Unauthenticated, "服务端未配置认证令牌")
	}
	header := r.Header.Get("Authorization")
	if len(header) <= len(bearerPrefix) ||
		!strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return Actor{}, errcode.New(errcode.Unauthenticated, "缺少 Bearer 令牌")
	}
	got := header[len(bearerPrefix):]
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) != 1 {
		return Actor{}, errcode.New(errcode.Unauthenticated, "令牌无效")
	}
	return Actor{UserID: s.UserID}, nil
}
