// Package commit 编排「一轮对话结束后的 git 自动提交 + turn_commits 记录」这一跨介质操作。
//
// 此前这段编排（打开仓库 → 检查未提交 → stage → commit → 写 turn_commits）散落在
// app/chat.go 绑定层，违反「app 不承载领域编排、跨介质用例应走
// App → internal/<domain>.Service → Store」的分层约定。现下沉到领域层：git 提交与
// turn_commits 落库统一在此协调，app 仅负责 DTO 与调用。
package commit

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/rollback"
)

// Service 编排对话轮次结束后的 git 自动提交与 turn_commits 记录。
type Service struct {
	rollback *rollback.Store
	logger   *slog.Logger
}

// NewService 创建提交编排服务。
func NewService(rollbackStore *rollback.Store, logger *slog.Logger) *Service {
	return &Service{rollback: rollbackStore, logger: logger}
}

// RecordUserChanges 在 turn 开始时提交用户在对话间隙对章节文件的手动修改。
// 仓库打开/无变更/提交失败均 best-effort 记日志后返回，不阻塞对话流程。
func (s *Service) RecordUserChanges(ctx context.Context, novelID int64, gitName, gitEmail, sessionID string, turnID int) {
	s.record(ctx, novelID, gitName, gitEmail, sessionID, turnID, "user", "")
}

// RecordAIChanges 在 turn 结束时提交 AI 对章节文件的所有修改（经 defer 调用，
// 确保正常结束 / 用户打断 / 异常退出都会执行）。
func (s *Service) RecordAIChanges(ctx context.Context, novelID int64, gitName, gitEmail, sessionID string, turnID int, modelName string) {
	s.record(ctx, novelID, gitName, gitEmail, sessionID, turnID, "ai", modelName)
}

// record 打开仓库并检查未提交变更，有则 stage+commit 并将 hash 写入 turn_commits。
// 全部 best-effort：任一环节失败仅记日志，绝不阻塞上层对话流程。
func (s *Service) record(ctx context.Context, novelID int64, gitName, gitEmail, sessionID string, turnID int, commitType, modelName string) {
	repo, err := git.New(novelID, gitName, gitEmail, s.logger)
	if err != nil {
		s.logger.Warn("auto-commit: 打开 git 仓库失败，跳过本轮自动提交", "err", err)
		return
	}
	has, err := repo.HasUncommitted()
	if err != nil {
		s.logger.Warn("auto-commit: 检查未提交变更失败", "err", err)
		return
	}
	if !has {
		return
	}
	if err := repo.StageAll(); err != nil {
		s.logger.Warn("auto-commit: git add 失败", "err", err)
		return
	}

	var msg string
	if commitType == "user" {
		msg = fmt.Sprintf("turn %d: user manual changes\n\nSession: %s", turnID, sessionID)
	} else {
		msg = fmt.Sprintf("turn %d: AI changes\n\nSession: %s\n\nCo-authored-by: NovelGo (%s)", turnID, sessionID, modelName)
	}

	hash, err := repo.Commit(msg)
	if err != nil {
		s.logger.Warn("auto-commit: git commit 失败", "err", err)
		return
	}
	if err := s.rollback.CreateTurnCommit(ctx, &rollback.TurnCommit{
		SessionID:  sessionID,
		TurnID:     turnID,
		CommitType: commitType,
		CommitHash: hash,
	}); err != nil {
		s.logger.Warn("auto-commit: 写入 turn_commits 失败", "err", err)
		return
	}
	s.logger.Info("auto-commit: 提交成功", "turn", turnID, "type", commitType, "hash", hash[:7])
}
