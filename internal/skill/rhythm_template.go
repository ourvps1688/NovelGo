package skill

// rhythm_template.go —— P2-2 平台化写作模板：scene-beats「平台节奏模板」可选节的
// 确定性提取与渲染（映射单源）。
//
// 模板正文住 scene-beats.md（scene-beats 的可选节奏模板，### <模板 ID> 小节），
// 本文件只做「模板 ID → 小节提取 → 【平台节奏模板】块」的纯函数转换；
// profile 未指名模板 / 小节缺失 / 读取失败 → 空串（缺省不模板，调用方零注入）。
//
// 纪律：模板只随有实证口径的平台 preset 发（与 quality.platform_profile.go
// 注册表纪律一致，当前仅 qimao_face_slap）；小节标题即模板 ID（勿改）。

import "strings"

// 节奏模板 ID（scene-beats.md「平台节奏模板」小节标题；quality.PlatformProfile
// 的 RhythmTemplate 字段指名，契约测试锁定两侧一致）。
const RhythmTemplateQimaoFaceSlap = "qimao_face_slap"

// RhythmBlock 按模板 ID 从 scene-beats.md 提取「平台节奏模板」小节并渲染注入块。
// 未知/空 ID、小节缺失/空正文 → 空串（缺省不模板）。同输入同输出。
func RhythmBlock(templateID string) string {
	id := strings.TrimSpace(templateID)
	if id == "" {
		return ""
	}
	raw, err := BuiltinFS.ReadFile("builtin/scene-beats.md")
	if err != nil {
		return ""
	}
	section := extractH3Section(string(raw), id)
	if section == "" {
		return ""
	}
	return "【平台节奏模板】本次写作按以下节奏模板执行（scene-beats 可选模板，可结合剧情取舍）：\n" + section
}

// extractH3Section 提取 `### <id>` 小节正文（去标题行，至下一个标题行或文末；
// 找不到小节或正文为空返回空串）。标题行匹配：TrimSpace 后 == "### "+id。
func extractH3Section(md, id string) string {
	lines := strings.Split(md, "\n")
	heading := "### " + id
	start := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == heading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}
	var body []string
	for _, ln := range lines[start:] {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "# ") || strings.HasPrefix(t, "## ") || strings.HasPrefix(t, "### ") {
			break
		}
		body = append(body, ln)
	}
	return strings.TrimSpace(strings.Join(body, "\n"))
}
