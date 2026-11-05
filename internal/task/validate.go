package task

import (
	"strings"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// MaxAgents 是普通模式单任务允许的 Agent 上限（dev_spec 1.2）。
const MaxAgents = 10

// v1 只支持单仓库（dev_spec 1.3）。
const maxRepositoriesV1 = 1

// normalizeAndValidate 做请求级校验；业务级校验（Agent 可调度、仓库归属）在事务内完成。
func normalizeAndValidate(req CreateRequest) (CreateRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return req, errcode.New(errcode.ValidationFailed, "name 必填")
	}

	if req.Mode == "" {
		req.Mode = domain.ModeOrdinary
	}
	if err := req.Mode.Valid(); err != nil {
		return req, errcode.New(errcode.ValidationFailed, err.Error())
	}
	if req.Mode == domain.ModeSquad {
		return req, errcode.New(errcode.ValidationFailed, "v1 仅支持 ordinary 模式")
	}

	if n := len(req.AgentIDs); n < 1 || n > MaxAgents {
		return req, errcode.Newf(errcode.ValidationFailed, "agentIds 数量必须在 1–%d 之间", MaxAgents)
	}
	seen := make(map[string]bool, len(req.AgentIDs))
	for _, id := range req.AgentIDs {
		if strings.TrimSpace(id) == "" {
			return req, errcode.New(errcode.ValidationFailed, "agentIds 不能包含空值")
		}
		if seen[id] {
			return req, errcode.Newf(errcode.ValidationFailed, "agentIds 重复: %s", id)
		}
		seen[id] = true
	}
	if req.PlannerAgentID == "" || !seen[req.PlannerAgentID] {
		return req, errcode.New(errcode.PlannerNotInMembers, "plannerAgentId 必须属于 agentIds")
	}

	if len(req.Repositories) > maxRepositoriesV1 {
		return req, errcode.Newf(errcode.ValidationFailed, "v1 仅支持 %d 个仓库", maxRepositoriesV1)
	}
	for _, repo := range req.Repositories {
		if strings.TrimSpace(repo) == "" {
			return req, errcode.New(errcode.ValidationFailed, "repositories 不能包含空值")
		}
	}
	return req, nil
}
