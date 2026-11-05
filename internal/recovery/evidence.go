package recovery

import (
	"context"
	"database/sql"
	"errors"

	"github.com/roarzx/my-aladin/internal/runtime"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
)

// Evidence 是对账取证结果（dev_spec 9.8.1 第 2 步）。
type Evidence struct {
	StepID     string `json:"stepId"`
	AttemptID  string `json:"attemptId"`
	NewCommits bool   `json:"newCommits"`
	// GitEvidenceKnown=false 表示机器离线/无响应，证据不足，禁止推断为“未执行”。
	GitEvidenceKnown     bool   `json:"gitEvidenceKnown"`
	CommitSHA            string `json:"commitSha,omitempty"`
	SummaryPresent       bool   `json:"summaryPresent"`
	SummaryDigest        string `json:"summaryDigest,omitempty"`
	SummaryDigestMatches bool   `json:"summaryDigestMatches"`
	MachineOnline        bool   `json:"machineOnline"`
	ExecutionRunning     bool   `json:"executionRunning"`
}

// EvidenceSource 负责收集对账证据；测试可替换。
type EvidenceSource interface {
	Collect(ctx context.Context, stepID string) (Evidence, error)
}

// DBSource 从服务端冷数据取证。
//
// Git 证据需要 daemon 的 get_git_diff / list_git_commits（普通模式 v1 未实现），
// 因此 GitEvidenceKnown 默认为 false：证据不足时一律转人工，不用超时推断“未执行”。
type DBSource struct {
	DB      *sql.DB
	Runtime runtime.Runtime
}

// Collect 实现 EvidenceSource。
func (s DBSource) Collect(ctx context.Context, stepID string) (Evidence, error) {
	evidence := Evidence{StepID: stepID}

	step, err := mysqlstore.GetStepByID(ctx, s.DB, stepID)
	if err != nil {
		return Evidence{}, err
	}
	plan, _, err := mysqlstore.GetPlanByID(ctx, s.DB, step.PlanID)
	if err != nil {
		return Evidence{}, err
	}

	attempt, err := mysqlstore.GetLatestAttempt(ctx, s.DB, stepID)
	if errors.Is(err, sql.ErrNoRows) {
		return evidence, nil
	}
	if err != nil {
		return Evidence{}, err
	}
	evidence.AttemptID = attempt.ID
	evidence.CommitSHA = attempt.CommitSHA

	versions, err := mysqlstore.ListContextVersions(ctx, s.DB, plan.TaskID, "step", stepID)
	if err != nil {
		return Evidence{}, err
	}
	latest := 0
	for _, entry := range versions {
		if entry.Kind == "step_summary" && entry.Version > latest {
			latest = entry.Version
		}
	}
	if latest > 0 {
		entry, err := mysqlstore.GetContextVersion(ctx, s.DB,
			plan.TaskID, "step", stepID, "step_summary", latest)
		if err != nil {
			return Evidence{}, err
		}
		evidence.SummaryPresent = true
		evidence.SummaryDigest = entry.ContentHash
		if attempt.ResultDigest != "" && entry.ContentHash == attempt.ResultDigest {
			evidence.SummaryDigestMatches = true
		}
	}

	if s.Runtime != nil && attempt.ExecutionID != "" {
		snapshot, err := s.Runtime.Inspect(ctx, attempt.ExecutionID)
		if err == nil {
			evidence.MachineOnline = true
			evidence.ExecutionRunning = snapshot.State == runtime.StateRunning
		}
	}
	return evidence, nil
}

// Verdict 是对账结论。
type Verdict string

const (
	VerdictCompleted   Verdict = "completed"
	VerdictFailed      Verdict = "failed"
	VerdictUndecidable Verdict = "undecidable"
)

// Decide 实现 9.8.1 判定表。机器仍在跑时绝不抢占，证据不足时不推断“未执行”。
func Decide(evidence Evidence) Verdict {
	if evidence.MachineOnline && evidence.ExecutionRunning {
		return VerdictUndecidable
	}
	if !evidence.GitEvidenceKnown {
		return VerdictUndecidable
	}
	switch {
	case evidence.NewCommits && evidence.SummaryPresent && evidence.SummaryDigestMatches:
		return VerdictCompleted
	case !evidence.NewCommits && !evidence.SummaryPresent:
		return VerdictFailed
	case evidence.NewCommits && !evidence.SummaryPresent:
		// 部分完成：保持 reconciling，转人工
		return VerdictUndecidable
	default:
		return VerdictUndecidable
	}
}
