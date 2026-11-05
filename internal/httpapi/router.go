package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Deps 是 Router 的装配依赖。
type Deps struct {
	Authenticator auth.Authenticator
	Members       MemberLookup
	Logger        *slog.Logger

	// Health / Ready 挂载在认证之外，供探针使用。
	Health http.HandlerFunc
	Ready  http.HandlerFunc

	// MountRoot 挂载在根路由上且不经过控制面认证，用于 WS 等自带鉴权的端点。
	MountRoot func(chi.Router)

	// RegisterGlobal 挂载在 / 下，已通过认证。
	RegisterGlobal func(chi.Router)
	// RegisterWorkspace 挂载在 /workspaces/{wid} 下，已通过认证与成员校验。
	RegisterWorkspace func(chi.Router)
}

// New 组装控制面路由。
//
// 中间件链：RequestID → TraceID → Recover；业务路由再叠加 Authenticate，
// /workspaces/{wid} 分组额外叠加 WorkspaceScope。
func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(RequestID, TraceID, Recover(d.Logger))

	if d.Health != nil {
		r.Get("/healthz", d.Health)
	}
	if d.Ready != nil {
		r.Get("/readyz", d.Ready)
	}
	if d.MountRoot != nil {
		d.MountRoot(r)
	}

	r.Group(func(pr chi.Router) {
		pr.Use(Authenticate(d.Authenticator))
		if d.RegisterGlobal != nil {
			d.RegisterGlobal(pr)
		}
		pr.Route("/workspaces/{wid}", func(wr chi.Router) {
			wr.Use(WorkspaceScope(d.Members))
			if d.RegisterWorkspace != nil {
				d.RegisterWorkspace(wr)
			}
		})
	})

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		errcode.Write(w, errcode.New(errcode.NotFound, "路由不存在"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		errcode.Write(w, errcode.New(errcode.NotFound, "路由或方法不存在"))
	})
	return r
}
