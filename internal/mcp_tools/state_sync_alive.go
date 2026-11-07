package mcp_tools

// statesync 的生死态派生接线（#103 第三层）：在派发子 Agent **之前**，本地把本章正文
// 明写的角色死亡单向落到 characters.alive，并把结果写进指令供 LLM 复核/补漏。
// 这样 alive 不再是「靠 LLM 自觉维护的独立列」，而是每章写后都由系统派生。

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/storage"
)

const aliveDerivationEmptyNote = "\n\n【生死态本地派生】本章正文未发现「角色名 + 完成态死亡谓词」的明确陈述" +
	"（本地规则刻意保守、宁漏勿误）。请读正文判断：若本章确有角色死亡，用 " +
	"update_character(alive=false) 补齐；不得把任何角色改回在世（复活须作者人工审批）。"

const aliveDerivationUnavailableNote = "\n\n【生死态本地派生】本次本地派生不可用（章节正文或台账读取失败），" +
	"请完全依据本章正文判断：确有角色死亡时用 update_character(alive=false) 补齐；" +
	"不得把任何角色改回在世（复活须作者人工审批）。"

// aliveDerivationNote 执行本地派生并返回注入 statesync 指令的说明段。
// db 为 nil 时优雅降级（不派生，但仍告知 LLM 生死态职责），任何失败都非阻断。
func aliveDerivationNote(ctx context.Context, db *gorm.DB, logger *slog.Logger, novelID, chapterID int64) string {
	if db == nil {
		return aliveDerivationUnavailableNote
	}
	content, err := git.ReadFile(novelID, git.ChapterPath(chapterID))
	if err != nil {
		if logger != nil {
			logger.Warn("statesync 生死态派生：读取本章正文失败（非阻断）",
				"err", err, "novelID", novelID, "chapterID", chapterID)
		}
		return aliveDerivationUnavailableNote
	}
	// 派生写入经 GORM 回调自动记 ledger_change_history；标 auto 与其余自动台账写一致。
	deriveCtx := storage.WithLedgerSource(ctx, storage.HistorySourceAuto)
	mentions, err := character.DeriveAliveFromChapter(deriveCtx, db, novelID, content)
	if err != nil {
		if logger != nil {
			logger.Warn("statesync 生死态派生失败（非阻断）",
				"err", err, "novelID", novelID, "chapterID", chapterID)
		}
		return aliveDerivationUnavailableNote
	}
	return formatAliveDerivationNote(mentions)
}

// formatAliveDerivationNote 把派生结果渲染成给 statesync 的复核指令。
func formatAliveDerivationNote(mentions []character.DeathMention) string {
	if len(mentions) == 0 {
		return aliveDerivationEmptyNote
	}
	var b strings.Builder
	b.WriteString("\n\n【生死态本地派生（系统已落库，无需你重复写入）】系统已从本章正文高精度扫描，" +
		"把以下角色单向标为已死（alive=false，变更已入台账履历）：\n")
	for i, m := range mentions {
		fmt.Fprintf(&b, "%d. #%d %s：%s\n", i+1, m.CharacterID, m.Name, m.Excerpt)
	}
	b.WriteString("请读本章正文复核：①确认名单确为本章完成的死亡事实；若疑似误标，" +
		"只在摘要里点名提示作者到角色台账人工改回，你不得复活（工具会拒绝）；" +
		"②本地规则刻意保守，若名单外还有角色在本章死亡，用 update_character(alive=false) 补齐。")
	return b.String()
}
