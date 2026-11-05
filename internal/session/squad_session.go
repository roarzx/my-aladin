package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/roarzx/my-aladin/internal/agent"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// ReuseOrCreate 返回小队协作会话：指纹匹配且已有 Provider 会话时复用，
// 否则创建新的 session_ordinal（F6）。
func (s *Service) ReuseOrCreate(ctx context.Context, taskID, agentID string) (mysqlstore.SessionRecord, bool, error) {
	cfg, err := mysqlstore.GetAgent(ctx, s.DB, agentID)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlstore.SessionRecord{}, false, errcode.New(errcode.AgentUnavailable, "Agent 不存在")
	}
	if err != nil {
		return mysqlstore.SessionRecord{}, false, errcode.New(errcode.UpstreamUnavailable, "读取 Agent 失败")
	}
	fingerprint := AgentFingerprint(cfg)

	latest, err := mysqlstore.FindSessionByKind(ctx, s.DB, taskID, agentID, "task_collaboration")
	if err == nil && CanResume(recordAdapter{record: latest}, fingerprint) {
		return latest, true, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return mysqlstore.SessionRecord{}, false, errcode.New(errcode.UpstreamUnavailable, "读取会话失败")
	}

	ordinal := 1
	if err == nil {
		ordinal = latest.Ordinal + 1
	}
	record, err := mysqlstore.CreateSessionWithFingerprint(ctx, s.DB, taskID, agentID,
		"task_collaboration", ordinal, fingerprint)
	if err != nil {
		return mysqlstore.SessionRecord{}, false, errcode.New(errcode.UpstreamUnavailable, "创建会话失败")
	}
	return record, false, nil
}

// AgentFingerprint 用 Agent 的执行语义（能力、环境、提示词、工作区、机器）计算指纹。
func AgentFingerprint(cfg agent.Agent) string {
	var env map[string]string
	_ = json.Unmarshal(cfg.EnvVars, &env)
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	envList := make([]string, 0, len(keys))
	for _, key := range keys {
		envList = append(envList, key+"="+env[key])
	}
	prompt := cfg.SystemPrompt + "\n" + cfg.WorkDir + "\n" + cfg.MachineID + "\n" + string(cfg.CapabilitySet)
	return Fingerprint("", envList, prompt)
}

type recordAdapter struct{ record mysqlstore.SessionRecord }

func (r recordAdapter) SessionFingerprint() string { return r.record.ResumeFingerprint }
