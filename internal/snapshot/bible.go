package snapshot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/novelgo/novelgo/internal/volume"
	"gorm.io/gorm"
)

// bibleBudgetBytes 「既定事实」注入 prompt 的字节预算：正常体量的系列圣经全量紧凑 JSON
// 远小于此；超出时按表粒度截断（registry 顺序=人物/关系/地点等高频判定依据靠前保留）。
const bibleBudgetBytes = 24 * 1024

// LatestSnapshot 返回该小说最近一次提交的卷快照（id 最大=最后冻结提交）。
// 无快照时返回 gorm.ErrRecordNotFound。
func (s *Service) LatestSnapshot(ctx context.Context, novelID int64) (*volume.VolumeSnapshot, error) {
	var snap volume.VolumeSnapshot
	if err := s.db.WithContext(ctx).Where("novel_id = ?", novelID).Order("id DESC").First(&snap).Error; err != nil {
		return nil, err
	}
	return &snap, nil
}

// EstablishedFactsText 渲染「已冻结卷既定事实」文本块（WBS 2.3：S4/S5 依据前卷快照校验）。
// 取最近一次冻结快照——采集口径是冻结时刻全量 series bible（递增全量），故最新一份即
// 已定稿卷提交后的固化状态。本书从未冻结过（无快照）返回空串，提示词与旧行为一致。
func (s *Service) EstablishedFactsText(ctx context.Context, novelID int64) (string, error) {
	snap, err := s.LatestSnapshot(ctx, novelID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return BibleText(snap, bibleBudgetBytes), nil
}

// BibleText 把快照 payload 渲染为「既定事实」文本（不含提示语引导，prompt 侧拼装）：
// 首行标注快照出处（卷/版本/提交时间），随后按 registry 表序输出各表紧凑 JSON。
// budget 为字节上限（<=0 不截断）；超预算按表粒度截断并标注，保证每行 JSON 自洽。
// payload 解析失败返回空串（宁可不注入也不喂坏数据）。
func BibleText(snap *volume.VolumeSnapshot, budget int) string {
	if snap == nil {
		return ""
	}
	payload, err := parsePayload(snap.Payload)
	if err != nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "（快照出处：volume_id=%d，snapshot_version=%d，committed_at=%s）\n",
		payload.VolumeID, payload.SnapshotVersion, payload.CommittedAt.Format(time.RFC3339))
	truncated := false
	for _, spec := range registry {
		raw, ok := payload.Tables[spec.name]
		if !ok || len(raw) == 0 || string(raw) == "null" {
			continue
		}
		line := spec.name + ": " + string(raw) + "\n"
		if budget > 0 && sb.Len()+len(line) > budget {
			truncated = true
			break
		}
		sb.WriteString(line)
	}
	if truncated {
		fmt.Fprintf(&sb, "……（超出 %d 字节预算，已按表截断，其余表未注入；需要时可用台账读取工具查现状）\n", budget)
	}
	return strings.TrimSpace(sb.String())
}
