package plan

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/httpapi"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

type planView struct {
	ID          string `json:"id"`
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	ContentHash string `json:"contentHash"`
	ContentMD   string `json:"contentMd"`
	Version     uint64 `json:"version"`
}

type stepView struct {
	ID       string `json:"id"`
	Ordinal  int    `json:"ordinal"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	AgentRef string `json:"agentRef"`
	Status   string `json:"status"`
	Version  uint64 `json:"version"`
}

// Get 处理 GET /issues/{id}/plans/{rev}；?allowMissing=true 时首次规划前返回空计划。
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	revision, err := strconv.Atoi(chi.URLParam(r, "rev"))
	if err != nil || revision < 1 {
		httpapi.Fail(w, errcode.New(errcode.ValidationFailed, "revision 不合法"))
		return
	}
	if _, _, err := h.authorize(r, taskID, auth.ActionView); err != nil {
		httpapi.Fail(w, err)
		return
	}

	p, steps, err := mysqlstore.GetPlanByRevision(r.Context(), h.DB, taskID, revision)
	if errors.Is(err, sql.ErrNoRows) {
		if r.URL.Query().Get("allowMissing") == "true" {
			httpapi.Data(w, http.StatusOK, map[string]any{"plan": nil, "steps": []stepView{}})
			return
		}
		httpapi.Fail(w, errcode.New(errcode.NotFound, "计划不存在"))
		return
	}
	if err != nil {
		httpapi.Fail(w, errcode.New(errcode.UpstreamUnavailable, "读取计划失败"))
		return
	}

	out := make([]stepView, 0, len(steps))
	for _, step := range steps {
		out = append(out, stepView{
			ID: step.ID, Ordinal: step.Ordinal, Title: step.Title,
			Body: step.Body, AgentRef: step.AgentRef,
			Status: string(step.Status), Version: step.Version,
		})
	}
	httpapi.Data(w, http.StatusOK, map[string]any{
		"plan": planView{
			ID: p.ID, Revision: p.Revision, Status: string(p.Status),
			ContentHash: p.ContentHash, ContentMD: p.ContentMD, Version: p.Version,
		},
		"steps": out,
	})
}
