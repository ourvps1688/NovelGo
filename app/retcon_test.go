package app

import (
	"testing"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/retcon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetconControlledEditLifecycle 受控编辑留痕全链（WBS 3.2 验收：每次 retcon
// 有 diff + 回滚点）：冻结卷 → 发起候选 → 显式批准 → 开窗固化回滚点 → 编辑台账 →
// 差异留痕收口（canon 刷新）→ 回滚到回滚点（台账还原、canon 再刷新）。
func TestRetconControlledEditLifecycle(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	name := "第一卷"
	v, err := app.PlaceVolume(PlaceVolumeInput{NovelID: novel.ID, Name: &name})
	require.NoError(t, err)

	// 冻结前造一条台账行，作为回改目标。
	ch := &character.Character{NovelID: novel.ID, Name: "陈三斗"}
	require.NoError(t, app.db.Create(ch).Error)
	_, err = app.SetVolumeFrozen(novel.ID, v.ID, true)
	require.NoError(t, err, "冻结应过台账审计并提交快照")

	// 发起候选（作者主动）→ 显式批准（必附理由）。
	c, err := app.CreateRetconCase(RetconCaseInput{
		NovelID: novel.ID, VolumeID: &v.ID, Title: "吃书：陈三斗改名", Direction: "retcon_candidate",
	})
	require.NoError(t, err)
	assert.Equal(t, retcon.StatusCandidate, c.Status)
	_, err = app.ApproveRetconCase(c.ID, "author", "")
	require.Error(t, err, "批准必须留痕理由")
	c, err = app.ApproveRetconCase(c.ID, "author", "确需回改核心身份，批准受控编辑")
	require.NoError(t, err)
	assert.Equal(t, retcon.StatusApproved, c.Status)

	// 开窗：固化回滚点快照（编辑前状态）。
	c, err = app.BeginRetconEdit(c.ID, v.ID, "author")
	require.NoError(t, err)
	require.NotNil(t, c.RollbackSnapshotID, "受控编辑必须有回滚点")
	assert.Equal(t, retcon.StatusEditing, c.Status)

	// 窗口内编辑台账（走普通写路径）。
	require.NoError(t, app.db.Model(&character.Character{}).
		Where("id = ?", ch.ID).Update("name", "陈四斗").Error)

	// 窗口内可查结构化差异（审批弹窗/编辑对照用）。
	delta, err := app.GetRetconEditDiff(c.ID)
	require.NoError(t, err)
	require.NotEmpty(t, delta.Tables, "回滚点 vs 当前应有差异")

	// 留痕收口：差异摘要固化 + canon 刷新。
	c, err = app.RecordRetconEdit(c.ID, "author")
	require.NoError(t, err)
	assert.Equal(t, retcon.StatusRecorded, c.Status)
	assert.Contains(t, c.DiffSummary, "name", "留痕应含字段级差异")

	// 回滚：台账整体还原到回滚点。
	c, err = app.RollbackRetconCase(c.ID, "author", "情节改道，撤销回改")
	require.NoError(t, err)
	assert.Equal(t, retcon.StatusRolledBack, c.Status)
	var got character.Character
	require.NoError(t, app.db.First(&got, ch.ID).Error)
	assert.Equal(t, "陈三斗", got.Name, "回滚应还原台账到回滚点")

	// 终态封死。
	_, err = app.RecordRetconEdit(c.ID, "author")
	require.Error(t, err, "终态不得再留痕")
}

// TestRetconCaseRequiresExplicitApproval 验证「retcon 必须显式审批」：不批准不得开
// 受控编辑窗口；未冻结卷不得发起。
func TestRetconCaseRequiresExplicitApproval(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	name := "第一卷"
	v, err := app.PlaceVolume(PlaceVolumeInput{NovelID: novel.ID, Name: &name})
	require.NoError(t, err)

	// 未冻结不得发起。
	_, err = app.CreateRetconCase(RetconCaseInput{NovelID: novel.ID, VolumeID: &v.ID, Title: "改"})
	require.Error(t, err, "retcon 只针对已冻结卷")

	_, err = app.SetVolumeFrozen(novel.ID, v.ID, true)
	require.NoError(t, err)
	c, err := app.CreateRetconCase(RetconCaseInput{NovelID: novel.ID, VolumeID: &v.ID, Title: "改"})
	require.NoError(t, err)

	// 未批准直接开窗 → 拒绝（状态机跳步）。
	_, err = app.BeginRetconEdit(c.ID, v.ID, "author")
	require.Error(t, err, "未经审批不得进入受控编辑")

	// 驳回后（终态）不得再批。
	_, err = app.RejectRetconCase(c.ID, "author", "前向适配已消解")
	require.NoError(t, err)
	_, err = app.ApproveRetconCase(c.ID, "author", "反悔批准")
	require.Error(t, err, "驳回终态不得再批")

	// 列表可见全量案件。
	rows, err := app.ListRetconCases(novel.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, retcon.StatusRejected, rows[0].Status)
}
