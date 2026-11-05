package squad

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/idgen"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Propose 提交工作项的新方案版本；修订后旧审批自动失效（新 proposal 无审批记录）。
//
// 返回创建后的方案；需要审批时 WorkItem 进入 awaiting_approval，否则直接 ready。
func Propose(ctx context.Context, db *sql.DB, workItemID, content string) (mysqlstore.Proposal, error) {
	item, err := mysqlstore.GetWorkItem(ctx, db, workItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return mysqlstore.Proposal{}, errcode.New(errcode.NotFound, "工作项不存在")
	}
	if err != nil {
		return mysqlstore.Proposal{}, errcode.New(errcode.UpstreamUnavailable, "读取工作项失败")
	}
	if item.Status.IsTerminal() {
		return mysqlstore.Proposal{}, errcode.New(errcode.AlreadyTerminal, "工作项已结束")
	}

	snapshot, err := mysqlstore.GetSquadSnapshot(ctx, db, item.TaskID)
	if err != nil {
		return mysqlstore.Proposal{}, errcode.New(errcode.NotFound, "任务没有冻结的小队快照")
	}
	if err := snapshot.ApprovalPolicy.Valid(); err != nil {
		return mysqlstore.Proposal{}, errcode.Newf(errcode.ValidationFailed, "审批配置不合法: %v", err)
	}

	latest, err := mysqlstore.LatestProposalRevision(ctx, db, workItemID)
	if err != nil {
		return mysqlstore.Proposal{}, err
	}
	sum := sha256.Sum256([]byte(content))
	proposal := mysqlstore.Proposal{
		ID: idgen.New(), WorkItemID: workItemID, Revision: latest + 1,
		Content: content, ContentHash: hex.EncodeToString(sum[:]),
	}
	if err := mysqlstore.InsertProposal(ctx, db, proposal); err != nil {
		return mysqlstore.Proposal{}, errcode.New(errcode.Conflict, "方案版本冲突，请重试")
	}

	// 状态机：proposed → reviewing → awaiting_approval →（无需审批时）ready
	if item.Status == domain.WorkItemProposed {
		if err := transition(ctx, db, &item, domain.WIReviewStarted); err != nil {
			return mysqlstore.Proposal{}, err
		}
	}
	if err := transition(ctx, db, &item, domain.WIApprovalRequested); err != nil {
		return mysqlstore.Proposal{}, err
	}

	if !snapshot.ApprovalPolicy.Required || len(snapshot.ApprovalPolicy.Chain) == 0 {
		if err := transition(ctx, db, &item, domain.WIApproved); err != nil {
			return mysqlstore.Proposal{}, err
		}
		return proposal, nil
	}

	for _, step := range snapshot.ApprovalPolicy.Chain {
		if err := mysqlstore.InsertApproval(ctx, db, mysqlstore.Approval{
			ID: idgen.New(), ProposalID: proposal.ID, StepOrder: step.Order,
			ApproverID: step.ApproverID, Decision: "pending",
		}); err != nil {
			return mysqlstore.Proposal{}, errcode.New(errcode.UpstreamUnavailable, "写入审批链失败")
		}
	}
	return proposal, nil
}

// Approve 按顺序通过审批；跳级、越权或旧版本审批均被拒绝。
func Approve(ctx context.Context, db *sql.DB, proposalID string, revision int, actorID uint64, order int) error {
	return decide(ctx, db, proposalID, revision, actorID, order, "approved", "")
}

// Reject 驳回审批，工作项回到 reviewing。
func Reject(ctx context.Context, db *sql.DB, proposalID string, revision int, actorID uint64, order int, comment string) error {
	return decide(ctx, db, proposalID, revision, actorID, order, "rejected", comment)
}

func decide(ctx context.Context, db *sql.DB, proposalID string, revision int,
	actorID uint64, order int, decision, comment string) error {
	proposal, err := mysqlstore.GetProposal(ctx, db, proposalID)
	if errors.Is(err, sql.ErrNoRows) {
		return errcode.New(errcode.NotFound, "方案不存在")
	}
	if err != nil {
		return errcode.New(errcode.UpstreamUnavailable, "读取方案失败")
	}
	if proposal.Revision != revision {
		return errcode.New(errcode.RevisionConflict, "方案版本不匹配")
	}
	// 修订后旧方案不可继续授权
	latest, err := mysqlstore.LatestProposalRevision(ctx, db, proposal.WorkItemID)
	if err != nil {
		return err
	}
	if latest != proposal.Revision {
		return errcode.New(errcode.PlanSuperseded, "方案已被新版本取代")
	}

	approvals, err := mysqlstore.ListApprovals(ctx, db, proposalID)
	if err != nil {
		return err
	}
	if len(approvals) == 0 {
		return errcode.New(errcode.ValidationFailed, "该方案没有审批链")
	}

	var target *mysqlstore.Approval
	for i := range approvals {
		if approvals[i].StepOrder == order {
			target = &approvals[i]
		}
	}
	if target == nil {
		return errcode.New(errcode.ValidationFailed, "审批顺序不存在")
	}
	if target.ApproverID != actorID {
		return errcode.New(errcode.Forbidden, "当前用户不是该步骤审批人")
	}
	if target.Decision != "pending" {
		return errcode.New(errcode.Conflict, "该审批步骤已处理")
	}
	for _, previous := range approvals {
		if previous.StepOrder < order && previous.Decision != "approved" {
			return errcode.New(errcode.Conflict, "审批链必须按顺序通过")
		}
	}
	if err := mysqlstore.DecideApproval(ctx, db, proposalID, order, decision, comment); err != nil {
		return err
	}

	item, err := mysqlstore.GetWorkItem(ctx, db, proposal.WorkItemID)
	if err != nil {
		return errcode.New(errcode.UpstreamUnavailable, "读取工作项失败")
	}
	if decision == "rejected" {
		if item.Status == domain.WorkItemAwaitingApproval {
			return transition(ctx, db, &item, domain.WIRejected)
		}
		return nil
	}
	for _, approval := range approvals {
		if approval.StepOrder == order {
			continue
		}
		if approval.Decision != "approved" {
			return nil
		}
	}
	if item.Status == domain.WorkItemAwaitingApproval {
		return transition(ctx, db, &item, domain.WIApproved)
	}
	return nil
}

func transition(ctx context.Context, db *sql.DB, item *domain.WorkItem, ev domain.WorkItemEvent) error {
	from := item.Status
	expected := item.Version
	if err := item.Apply(ev); err != nil {
		return errcode.New(errcode.Conflict, err.Error())
	}
	version, err := mysqlstore.UpdateWorkItemStatus(ctx, db, item.ID, from, item.Status, expected)
	if err != nil {
		return err
	}
	item.Version = version
	return nil
}
