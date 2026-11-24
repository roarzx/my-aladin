package stats

import (
	"net/http"
	"strconv"

	"github.com/roarzx/my-aladin/internal/httpapi"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Handler 暴露统计接口。
type Handler struct {
	Service *Service
}

// Overview 返回窗口聚合结果，供前端看板使用（需认证）。
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	days := 7
	if raw := r.URL.Query().Get("days"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "days 必须是正整数"))
			return
		}
		days = value
	}
	overview, err := h.Service.Overview(r.Context(), days)
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "聚合监控数据失败"))
		return
	}
	httpapi.Data(w, http.StatusOK, overview)
}

// Metrics 输出 Prometheus 文本格式（不鉴权，供抓取）。
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	overview, err := h.Service.Overview(r.Context(), 7)
	if err != nil {
		http.Error(w, "aggregate: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(RenderPrometheus(overview)))
}
