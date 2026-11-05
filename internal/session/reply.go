package session

import (
	"context"
	"database/sql"

	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	"github.com/roarzx/my-aladin/pkg/errcode"
)

// ReplyService 管理回复生命周期（D4）。
type ReplyService struct {
	DB *sql.DB
}

// Open 开启回复；同一 source_message 已有未终态回复时返回已有记录。
func (s *ReplyService) Open(ctx context.Context, record mysqlstore.ReplyRecord) (mysqlstore.ReplyRecord, error) {
	if record.SourceMessageID == "" {
		return mysqlstore.ReplyRecord{}, errcode.New(errcode.ValidationFailed, "sourceMessageId 必填")
	}
	opened, err := mysqlstore.OpenReply(ctx, s.DB, record)
	if err != nil {
		return mysqlstore.ReplyRecord{}, errcode.New(errcode.UpstreamUnavailable, "创建回复记录失败")
	}
	return opened, nil
}

// Close 关闭回复；重复关闭幂等。
func (s *ReplyService) Close(ctx context.Context, id, phase string) error {
	if phase == "" {
		return errcode.New(errcode.ValidationFailed, "phase 必填")
	}
	if err := mysqlstore.CloseReply(ctx, s.DB, id, phase); err != nil {
		return errcode.New(errcode.UpstreamUnavailable, "关闭回复失败")
	}
	return nil
}
