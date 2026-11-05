package task

import (
	"testing"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

func validRequest() CreateRequest {
	return CreateRequest{
		Name:           "迁移到新路由",
		Mode:           domain.ModeOrdinary,
		AgentIDs:       []string{"agent-1", "agent-2"},
		PlannerAgentID: "agent-1",
		Repositories:   []string{"repo-1"},
	}
}

func TestNormalizeAndValidate(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*CreateRequest)
		wantCode errcode.Code
		wantOK   bool
		checkReq func(*testing.T, CreateRequest)
	}{
		{name: "合法请求", wantOK: true},
		{
			name:   "默认模式为 ordinary",
			mutate: func(r *CreateRequest) { r.Mode = "" },
			wantOK: true,
			checkReq: func(t *testing.T, r CreateRequest) {
				if r.Mode != domain.ModeOrdinary {
					t.Fatalf("Mode = %q", r.Mode)
				}
			},
		},
		{
			name:     "name 为空",
			mutate:   func(r *CreateRequest) { r.Name = "   " },
			wantCode: errcode.ValidationFailed,
		},
		{
			name: "squad 模式缺少 squadId",
			mutate: func(r *CreateRequest) {
				r.Mode = domain.ModeSquad
				r.AgentIDs = nil
				r.PlannerAgentID = ""
			},
			wantCode: errcode.ValidationFailed,
		},
		{
			name: "squadId 与 agentIds 互斥",
			mutate: func(r *CreateRequest) {
				r.Mode = domain.ModeSquad
				r.SquadID = "squad-1"
			},
			wantCode: errcode.ValidationFailed,
		},
		{
			name: "ordinary 模式带 squadId",
			mutate: func(r *CreateRequest) {
				r.SquadID = "squad-1"
			},
			wantCode: errcode.ValidationFailed,
		},
		{
			name: "合法 squad 请求",
			mutate: func(r *CreateRequest) {
				r.Mode = domain.ModeSquad
				r.SquadID = "squad-1"
				r.AgentIDs = nil
				r.PlannerAgentID = ""
			},
			wantOK: true,
		},
		{
			name:     "没有 Agent",
			mutate:   func(r *CreateRequest) { r.AgentIDs = nil },
			wantCode: errcode.ValidationFailed,
		},
		{
			name: "超过 10 个 Agent",
			mutate: func(r *CreateRequest) {
				r.AgentIDs = r.AgentIDs[:0]
				for i := 0; i < MaxAgents+1; i++ {
					r.AgentIDs = append(r.AgentIDs, "agent-x")
				}
			},
			wantCode: errcode.ValidationFailed,
		},
		{
			name:     "Agent 重复",
			mutate:   func(r *CreateRequest) { r.AgentIDs = []string{"agent-1", "agent-1"} },
			wantCode: errcode.ValidationFailed,
		},
		{
			name:     "planner 不在成员内",
			mutate:   func(r *CreateRequest) { r.PlannerAgentID = "agent-9" },
			wantCode: errcode.PlannerNotInMembers,
		},
		{
			name:     "v1 仅支持单仓库",
			mutate:   func(r *CreateRequest) { r.Repositories = []string{"r1", "r2"} },
			wantCode: errcode.ValidationFailed,
		},
		{
			name:     "仓库 ID 为空",
			mutate:   func(r *CreateRequest) { r.Repositories = []string{" "} },
			wantCode: errcode.ValidationFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			if tc.mutate != nil {
				tc.mutate(&req)
			}
			got, err := normalizeAndValidate(req)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("期望通过: %v", err)
				}
				if tc.checkReq != nil {
					tc.checkReq(t, got)
				}
				return
			}
			if err == nil {
				t.Fatal("期望校验失败")
			}
			e, ok := errcode.As(err)
			if !ok || e.Code != tc.wantCode {
				t.Fatalf("错误码 = %v, 期望 %s", err, tc.wantCode)
			}
		})
	}
}
