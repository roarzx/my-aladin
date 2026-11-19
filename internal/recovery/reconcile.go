package recovery

import (
	"context"
	"database/sql"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/orchestration"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Reconciler 执行 9.8.1 的取证 → 判定 → 裁决（E6）。
type Reconciler struct {
	DB       *sql.DB
	Source   EvidenceSource
	Handlers *orchestration.EventHandler
}

// Reconcile 对指定步骤取证并按判定表落状态；无法判定时保持 reconciling。
func (r *Reconciler) Reconcile(ctx context.Context, stepID string) (Verdict, Evidence, error) {
	source := r.Source
	if source == nil {
		source = DBSource{DB: r.DB}
	}
	evidence, err := source.Collect(ctx, stepID)
	if err != nil {
		return "", Evidence{}, err
	}
	verdict := Decide(evidence)
	if verdict == VerdictUndecidable {
		r.record(ctx, stepID, "step.reconcile_pending", map[string]any{
			"reason": "evidence_insufficient",
		})
		return verdict, evidence, nil
	}
	if err := r.apply(ctx, stepID, verdict, evidence.SummaryDigest, evidence.CommitSHA); err != nil {
		return "", Evidence{}, err
	}
	return verdict, evidence, nil
}

// ApplyVerdict 写人工裁决（POST /steps/{id}/reconcile）。
func (r *Reconciler) ApplyVerdict(ctx context.Context, stepID string, verdict Verdict, evidence string) error {
	if verdict != VerdictCompleted && verdict != VerdictFailed {
		return errcode.New(errcode.ValidationFailed, "裁决只能是 completed/failed")
	}
	attempt, err := mysqlstore.GetLatestAttempt(ctx, r.DB, stepID)
	if err != nil {
		return errcode.New(errcode.NotFound, "步骤没有执行尝试")
	}
	if err := mysqlstore.SetReconcileVerdict(ctx, r.DB, attempt.ID, string(verdict)); err != nil {
		return err
	}
	if err := r.apply(ctx, stepID, verdict, attempt.ResultDigest, attempt.CommitSHA); err != nil {
		return err
	}
	r.record(ctx, stepID, "step.reconciled", map[string]any{
		"verdict": string(verdict), "manual": true, "evidence": evidence,
	})
	return nil
}

func (r *Reconciler) apply(ctx context.Context, stepID string, verdict Verdict,
	digest, commitSHA string) error {
	step, err := mysqlstore.GetStepByID(ctx, r.DB, stepID)
	if err != nil {
		return errcode.New(errcode.NotFound, "步骤不存在")
	}
	if step.Status != domain.StepReconciling {
		return errcode.Newf(errcode.StepNotReconciling, "步骤状态为 %s，不在对账中", step.Status)
	}
	attempt, err := mysqlstore.GetLatestAttempt(ctx, r.DB, stepID)
	if err != nil {
		return errcode.New(errcode.NotFound, "步骤没有执行尝试")
	}

	stepTo := domain.StepCompleted
	attemptTo := domain.AttemptCompleted
	if verdict == VerdictFailed {
		stepTo = domain.StepFailed
		attemptTo = domain.AttemptFailed
	}
	if err := mysqlstore.UpdateAttemptStatus(ctx, r.DB, attempt.ID, attemptTo, "", digest, commitSHA); err != nil {
		return err
	}
	if _, err := mysqlstore.UpdateStepState(ctx, r.DB, step.ID,
		domain.StepReconciling, stepTo, step.Version); err != nil {
		return err
	}
	r.record(ctx, stepID, "step.reconciled", map[string]any{"verdict": string(verdict)})

	if r.Handlers == nil {
		return nil
	}
	if verdict == VerdictCompleted {
		// 对账期间计划/任务处于 paused；先恢复 executing，AdvancePlan 才能把
		// 全终态的步骤收敛为 completed，否则任务会一直停在 paused。
		r.resume(ctx, step.PlanID)
		return r.Handlers.AdvancePlan(ctx, step.PlanID)
	}
	return r.Handlers.PausePlan(ctx, step.PlanID)
}

// resume 把因对账而暂停的计划与任务恢复为 executing（尽量而为）。
func (r *Reconciler) resume(ctx context.Context, planID string) {
	plan, _, err := mysqlstore.GetPlanByID(ctx, r.DB, planID)
	if err != nil {
		return
	}
	if plan.Status == domain.PlanPaused {
		_, _ = mysqlstore.UpdatePlanState(ctx, r.DB, plan.ID,
			domain.PlanPaused, domain.PlanExecuting, plan.Version)
	}
	task, err := mysqlstore.GetTask(ctx, r.DB, plan.TaskID)
	if err != nil {
		return
	}
	if task.Status == domain.TaskPaused {
		_, _ = mysqlstore.UpdateTaskState(ctx, r.DB, task.ID,
			domain.TaskPaused, domain.TaskExecuting, task.Version)
	}
}

func (r *Reconciler) record(ctx context.Context, stepID, kind string, payload map[string]any) {
	step, err := mysqlstore.GetStepByID(ctx, r.DB, stepID)
	if err != nil {
		return
	}
	plan, _, err := mysqlstore.GetPlanByID(ctx, r.DB, step.PlanID)
	if err != nil {
		return
	}
	payload["step_id"] = stepID
	_, _ = mysqlstore.AppendActivity(ctx, r.DB, plan.TaskID, kind, payload)
}
