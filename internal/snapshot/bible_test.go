package snapshot

import (
	"context"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
)

// TestEstablishedFactsText 验证「已冻结卷既定事实」渲染：无快照为空串；
// 冻结提交后含快照出处（版本/提交时间）与各表紧凑 JSON 行（registry 序）。
func TestEstablishedFactsText(t *testing.T) {
	db := openSnapDB(t)
	svc := NewService(db, volume.NewStore(db, testSnapLogger()), chapter.NewStore(db, testSnapLogger()))
	ctx := context.Background()
	novelID := int64(1)
	volID := seedFixture(t, db)

	// 无快照：空串（提示词与旧行为一致）。
	text, err := svc.EstablishedFactsText(ctx, novelID)
	require.NoError(t, err)
	require.Empty(t, text)

	_, snap, err := svc.Freeze(ctx, novelID, volID)
	require.NoError(t, err)

	text, err = svc.EstablishedFactsText(ctx, novelID)
	require.NoError(t, err)
	require.Contains(t, text, "snapshot_version=1", "应标注快照版本")
	require.Contains(t, text, "committed_at=", "应标注提交时间")
	require.Contains(t, text, "characters:", "应按表输出台账")
	require.Contains(t, text, "reader_perspectives:", "应含读者视角表")
	require.Contains(t, text, "主角", "应含行内容")
	require.True(t, strings.Index(text, "characters:") < strings.Index(text, "reader_perspectives:"),
		"表序应按 registry（characters 先于 reader_perspectives）")
	require.NotContains(t, text, "chapter_manifest", "章节清单不入注入块（agent 经 get_chapter_list 读）")

	// BibleText 与 EstablishedFactsText 同源。
	require.Equal(t, BibleText(snap, bibleBudgetBytes), text)
}

// TestBibleTextTruncation 验证超预算按表粒度截断：靠前表保留、截断标注存在、坏 payload 不注入。
func TestBibleTextTruncation(t *testing.T) {
	db := openSnapDB(t)
	svc := NewService(db, volume.NewStore(db, testSnapLogger()), chapter.NewStore(db, testSnapLogger()))
	ctx := context.Background()
	volID := seedFixture(t, db)
	_, snap, err := svc.Freeze(ctx, 1, volID)
	require.NoError(t, err)

	// 预算只够放头部 + 靠前的表：后续表被截断。
	small := BibleText(snap, 200)
	require.Contains(t, small, "已按表截断", "超预算应标注截断")
	require.NotContains(t, small, "reader_perspectives:", "截断后不应含靠后的表")

	full := BibleText(snap, 0)
	require.NotContains(t, full, "已按表截断", "budget<=0 不截断")
	require.Contains(t, full, "reader_perspectives:")

	// 坏 payload：宁可不注入也不喂坏数据。
	require.Empty(t, BibleText(&volume.VolumeSnapshot{Payload: "{not json"}, 0))
}
