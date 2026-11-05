package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/roarzx/my-aladin/pkg/errcode"
)

// Proposal 是 work_items 的方案版本。
type Proposal struct {
	ID          string
	WorkItemID  string
	Revision    int
	Content     string
	ContentHash string
}

// Approval 是审批链中的一行。
type Approval struct {
	ID         string
	ProposalID string
	StepOrder  int
	ApproverID uint64
	Decision   string
	Comment    string
	DecidedAt  *time.Time
}

// InsertProposal 写入方案版本。
func InsertProposal(ctx context.Context, ex Executor, p Proposal) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO proposals (id, work_item_id, revision, content, content_hash)
		VALUES (?, ?, ?, ?, ?)`,
		p.ID, p.WorkItemID, p.Revision, p.Content, p.ContentHash)
	if err != nil {
		return fmt.Errorf("写入方案: %w", err)
	}
	return nil
}

// GetProposal 读取方案。
func GetProposal(ctx context.Context, ex Executor, id string) (Proposal, error) {
	var p Proposal
	err := ex.QueryRowContext(ctx, `
		SELECT id, work_item_id, revision, content, content_hash
		FROM proposals WHERE id = ?`, id).Scan(
		&p.ID, &p.WorkItemID, &p.Revision, &p.Content, &p.ContentHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, sql.ErrNoRows
	}
	if err != nil {
		return Proposal{}, fmt.Errorf("读取方案: %w", err)
	}
	return p, nil
}

// LatestProposalRevision 返回工作项的最新方案版本（0 表示没有）。
func LatestProposalRevision(ctx context.Context, ex Executor, workItemID string) (int, error) {
	var revision sql.NullInt64
	if err := ex.QueryRowContext(ctx,
		`SELECT MAX(revision) FROM proposals WHERE work_item_id = ?`, workItemID).Scan(&revision); err != nil {
		return 0, err
	}
	if !revision.Valid {
		return 0, nil
	}
	return int(revision.Int64), nil
}

// InsertApproval 写入审批链的一步。
func InsertApproval(ctx context.Context, ex Executor, a Approval) error {
	_, err := ex.ExecContext(ctx, `
		INSERT INTO approvals (id, proposal_id, step_order, approver_id, decision)
		VALUES (?, ?, ?, ?, ?)`,
		a.ID, a.ProposalID, a.StepOrder, a.ApproverID, a.Decision)
	if err != nil {
		return fmt.Errorf("写入审批: %w", err)
	}
	return nil
}

// ListApprovals 按顺序列出审批。
func ListApprovals(ctx context.Context, ex Executor, proposalID string) ([]Approval, error) {
	rows, err := ex.QueryContext(ctx, `
		SELECT id, proposal_id, step_order, approver_id, decision, COALESCE(comment, ''), decided_at
		FROM approvals WHERE proposal_id = ? ORDER BY step_order`, proposalID)
	if err != nil {
		return nil, fmt.Errorf("列出审批: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Approval
	for rows.Next() {
		var (
			a         Approval
			decidedAt sql.NullTime
		)
		if err := rows.Scan(&a.ID, &a.ProposalID, &a.StepOrder, &a.ApproverID,
			&a.Decision, &a.Comment, &decidedAt); err != nil {
			return nil, err
		}
		if decidedAt.Valid {
			t := decidedAt.Time
			a.DecidedAt = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DecideApproval 记录某个顺序的审批结论；仅 pending 可变更。
func DecideApproval(ctx context.Context, ex Executor, proposalID string, order int,
	decision, comment string) error {
	if decision != "approved" && decision != "rejected" {
		return fmt.Errorf("审批结论只能是 approved/rejected")
	}
	res, err := ex.ExecContext(ctx, `
		UPDATE approvals SET decision = ?, comment = ?, decided_at = UTC_TIMESTAMP(3)
		WHERE proposal_id = ? AND step_order = ? AND decision = 'pending'`,
		decision, nullString(comment), proposalID, order)
	if err != nil {
		return fmt.Errorf("更新审批: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errcode.New(errcode.Conflict, "审批步骤已处理或不存在")
	}
	return nil
}
