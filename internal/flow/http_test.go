//go:build integration

package flow_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/roarzx/my-aladin/internal/daemon"
	"github.com/roarzx/my-aladin/internal/flow"
	"github.com/roarzx/my-aladin/internal/httpapi"
)

func TestFlowNodeMachineAPI(t *testing.T) {
	db, wsID := setupFlow(t)
	ctx := context.Background()
	tpl, err := flow.Publish(ctx, db, wsID, "验收", flow.DefaultNodes(), 0)
	if err != nil {
		t.Fatal(err)
	}
	taskID, cleanup := createFlowTask(t, db, wsID)
	t.Cleanup(cleanup)
	run, _, err := flow.CreateRun(ctx, db, taskID, tpl.ID, json.RawMessage(`{"prompt":"验收"}`))
	if err != nil {
		t.Fatal(err)
	}
	runner := &flow.FlowRunner{DB: db, Nodes: flow.DefaultExecutors()}
	if err := runner.RunOnce(ctx, run.ID); err != nil { // env 完成
		t.Fatal(err)
	}

	handler := &flow.Handler{DB: db, TTL: time.Minute}
	router := chi.NewRouter()
	router.Route("/api/v1/cli", func(r chi.Router) {
		r.Use(httpapi.MachineAuth(func(*http.Request) (string, error) { return "machine-1", nil }))
		r.Post("/flow-run-nodes/claim", handler.Claim)
		r.Post("/flow-run-nodes/{id}/heartbeat", handler.Heartbeat)
		r.Post("/flow-run-nodes/{id}/complete", handler.Complete)
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	client := daemon.NewCLIClient(srv.URL, "token")
	claim, ok, err := client.ClaimFlowNode(ctx)
	if err != nil || !ok || claim == nil {
		t.Fatalf("Claim: %+v, ok=%v, err=%v", claim, ok, err)
	}
	if claim.NodeKey != "agent_prompt" || claim.LeaseEpoch != 1 || claim.TaskID != taskID {
		t.Fatalf("claim = %+v", claim)
	}
	if err := client.HeartbeatFlowNode(ctx, claim.NodeID, claim.LeaseEpoch); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	// 旧 epoch 的完成上报被拒绝
	if err := client.CompleteFlowNode(ctx, claim.NodeID, claim.LeaseEpoch+1, json.RawMessage(`{}`)); err == nil {
		t.Fatal("旧 epoch 完成应被拒绝")
	}
	if err := client.CompleteFlowNode(ctx, claim.NodeID, claim.LeaseEpoch,
		json.RawMessage(`{"summary":"ok"}`)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := runner.RunOnce(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	got, _, err := flow.GetRun(ctx, db, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" {
		t.Fatalf("run = %q", got.Status)
	}
}
