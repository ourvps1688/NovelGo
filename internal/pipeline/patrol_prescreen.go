package pipeline

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/rag"
)

// ── 一致性巡检·向量/确定性预筛（Phase 2：混合方案第 2 层）────────────────────
//
// 在分块 Map-Reduce 之前，先用全文向量把「语义相近、跨章、且数值/事实可能分歧」的陈述聚类成
// 候选矛盾簇，生成一份**短清单**喂给归并（Reduce）步，让 LLM 优先核对这些点，而不是盲扫全书。
//
// 设计要点：
//   - 只抽「疑似主张句」（含数字/单位/比较词），控住嵌入与聚类规模（patrolPrescreenCap）。
//   - 用 BGE 512 维向量做单链聚类（余弦 ≥ patrolPrescreenSim），仅保留跨章簇——单章近义句无意义。
//   - 簇内做确定性分歧检测（同单位家族、数值不同 → Divergent），作为排序与提示信号。
//   - 无 embedder（app 未初始化 RAG）时整体跳过：优雅降级，不阻断巡检。
//   - embedder 取全局单例 rag.TryGetEmbedder()（架构允许的「唯一全局单例例外」）；
//     测试通过 patrolEmbedderProvider 注入 stub，不依赖真实 ONNX 模型。

// prescreenEmbedder 是预筛所需的 embedder 最小接口（避免与 rag.Embedder 强耦合）。
type prescreenEmbedder interface {
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dim() int
}

// patrolEmbedderProvider 取向量 embedder；测试可覆盖注入 stub。
var patrolEmbedderProvider = func() prescreenEmbedder { return rag.TryGetEmbedder() }

const (
	patrolPrescreenCap         = 1200 // 候选陈述上限（句子级抽取后截断），控嵌入 + 聚类规模
	patrolPrescreenPerChapter  = 40   // 单章候选上限
	patrolPrescreenSim         = 0.78 // 余弦相似度聚类阈值（BGE 512 维，L2 归一）
	patrolPrescreenSentenceCap = 220  // 单句最大 rune 数（超长截断，避免噪声句）
	patrolPrescreenBatch       = 256  // 单次 EmbedBatch 批大小
	patrolPrescreenTopClusters = 24   // 喂给归并步的候选簇上限
	patrolPrescreenMinCluster  = 2    // 成簇所需的最小候选数（<2 不成簇）
)

var (
	// reNumWord 匹配前导数字（阿拉伯 / 中文数词）。
	reNumWord = regexp.MustCompile(`(?:[0-9]+(?:\.[0-9]+)?|零|一|二|两|三|四|五|六|七|八|九|十|百|千|万|亿|半)+`)
	// reUnit 计量单位家族。
	reUnit = `(?:年|个月|月|天|日|小时|时|分钟|分|秒|周|岁|元|块|个|次|倍|成|%|％|级|层|丈|尺|寸|斤|公斤|千克|米|厘米|公里|千米)`
	// reValue 抽取数值 token（数字 + 可选单位）。
	reValue = regexp.MustCompile(`(?:[0-9]+(?:\.[0-9]+)?|零|一|二|两|三|四|五|六|七|八|九|十|百|千|万|亿|半)+` + reUnit)
	// reClaim 判定句子是否「疑似主张」（含数字或比较/量级词），仅抽取此类句以控规模。
	reClaim = regexp.MustCompile(`[0-9零一二两三四五六七八九十百千万亿半]|比|超过|不足|不到|仅|整整|刚好|相差|多|少|倍|余|约|大约|将近|掉|升|降|涨|跌|增|减`)
)

// PrescreenCand 预筛抽取的一条候选陈述（来自某章某句）。
type PrescreenCand struct {
	ReadingNumber int
	ChapterTitle  string
	Sentence      string
	Values        []string
}

// PrescreenCluster 语义相近、跨章的候选矛盾簇。
type PrescreenCluster struct {
	Cands     []PrescreenCand
	Sim       float64
	Divergent bool
	Values    []string
}

// PrescreenReport 预筛结果；Ran=false 表示跳过（无 embedder / 候选不足 / 未聚类出跨章簇）。
type PrescreenReport struct {
	Ran      bool
	Reason   string
	Clusters []PrescreenCluster
}

// splitPrescreenSentences 把正文切成句子（按句末标点 / 换行；超长截断）。
func splitPrescreenSentences(text string) []string {
	raw := strings.FieldsFunc(text, func(r rune) bool {
		return r == '。' || r == '！' || r == '？' || r == '!' || r == '?' ||
			r == '\n' || r == '\r' || r == '；' || r == ';'
	})
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		runes := []rune(s)
		if len(runes) > patrolPrescreenSentenceCap {
			s = string(runes[:patrolPrescreenSentenceCap])
		}
		out = append(out, s)
	}
	return out
}

// extractPrescreenCands 逐章取正文、切句、抽候选（仅疑似主张句，受单章/总量上限约束）。
func (s *Service) extractPrescreenCands(ctx context.Context, novelID int64, chapters []chapter.Chapter) []PrescreenCand {
	var cands []PrescreenCand
	for i, ch := range chapters {
		rn := ch.ReadingNumber
		if rn == 0 {
			rn = i + 1
		}
		text := ""
		if s.chapterTextReader != nil {
			if t, rerr := s.chapterTextReader(ctx, novelID, ch.ID); rerr == nil {
				text = t
			}
		}
		if text == "" {
			continue
		}
		per := 0
		for _, sent := range splitPrescreenSentences(text) {
			if per >= patrolPrescreenPerChapter {
				break
			}
			if !reClaim.MatchString(sent) {
				continue
			}
			cands = append(cands, PrescreenCand{
				ReadingNumber: rn,
				ChapterTitle:  ch.Title,
				Sentence:      sent,
				Values:        reValue.FindAllString(sent, -1),
			})
			per++
			if len(cands) >= patrolPrescreenCap {
				return cands
			}
		}
	}
	return cands
}

// cosineSim 计算两个（已 L2 归一化或原始）向量的余弦相似度。
func cosineSim(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := 0; i < len(a) && i < len(b); i++ {
		x := float64(a[i])
		y := float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// clusterPrescreenCands 批量嵌入候选句并按余弦相似度单链聚类，仅保留跨章簇。
func clusterPrescreenCands(ctx context.Context, emb prescreenEmbedder, cands []PrescreenCand) ([]PrescreenCluster, error) {
	if len(cands) < patrolPrescreenMinCluster {
		return nil, nil
	}
	vecs := make([][]float32, 0, len(cands))
	for i := 0; i < len(cands); i += patrolPrescreenBatch {
		end := i + patrolPrescreenBatch
		if end > len(cands) {
			end = len(cands)
		}
		batch := make([]string, 0, end-i)
		for j := i; j < end; j++ {
			batch = append(batch, cands[j].Sentence)
		}
		v, err := emb.EmbedBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		vecs = append(vecs, v...)
	}
	if len(vecs) != len(cands) {
		return nil, fmt.Errorf("嵌入数量不匹配：want %d got %d", len(cands), len(vecs))
	}

	// 单链聚类（并查集）。
	parent := make([]int, len(cands))
	sims := make([]float64, len(cands))
	for i := range parent {
		parent[i] = i
	}
	find := func(x int) int {
		for parent[x] != x {
			x = parent[x]
		}
		return x
	}
	for i := 0; i < len(cands); i++ {
		for j := i + 1; j < len(cands); j++ {
			sim := cosineSim(vecs[i], vecs[j])
			if sim < patrolPrescreenSim {
				continue
			}
			a, b := find(i), find(j)
			if a != b {
				parent[a] = b
			}
			if sims[b] == 0 || sim < sims[b] {
				sims[b] = sim
			}
		}
	}

	groups := map[int][]int{}
	for i := range cands {
		groups[find(i)] = append(groups[find(i)], i)
	}

	var clusters []PrescreenCluster
	for _, idxs := range groups {
		if len(idxs) < patrolPrescreenMinCluster {
			continue
		}
		seen := map[int]bool{}
		var cc []PrescreenCand
		var vals []string
		for _, idx := range idxs {
			c := cands[idx]
			cc = append(cc, c)
			seen[c.ReadingNumber] = true
			vals = append(vals, c.Values...)
		}
		if len(seen) < 2 {
			continue // 必须跨章
		}
		clusters = append(clusters, PrescreenCluster{
			Cands:     cc,
			Sim:       sims[find(idxs[0])],
			Divergent: detectDivergence(vals),
			Values:    dedupeStrings(vals),
		})
	}

	sort.Slice(clusters, func(a, b int) bool {
		if clusters[a].Divergent != clusters[b].Divergent {
			return clusters[a].Divergent // 数值分歧优先
		}
		if len(clusters[a].Cands) != len(clusters[b].Cands) {
			return len(clusters[a].Cands) > len(clusters[b].Cands) // 簇大优先
		}
		return clusters[a].Sim > clusters[b].Sim // 更紧的簇优先
	})
	if len(clusters) > patrolPrescreenTopClusters {
		clusters = clusters[:patrolPrescreenTopClusters]
	}
	return clusters, nil
}

// detectDivergence 确定性分歧检测：同一单位家族内出现不同数值 → 分歧。
func detectDivergence(vals []string) bool {
	byUnit := map[string][]float64{}
	for _, v := range vals {
		mag, unit, ok := parseCNNumber(v)
		if !ok || unit == "" {
			continue
		}
		byUnit[unit] = append(byUnit[unit], mag)
	}
	for _, ms := range byUnit {
		if len(ms) < 2 {
			continue
		}
		mn, mx := ms[0], ms[0]
		for _, m := range ms {
			if m < mn {
				mn = m
			}
			if m > mx {
				mx = m
			}
		}
		if mx-mn > 1e-9 {
			return true
		}
	}
	return false
}

// parseCNNumber 解析数值 token 为（量值, 单位家族, ok）。
// 支持阿拉伯数字与常见中文数词 + 十/百/千/万/亿 乘子；无法解析返回 ok=false。
func parseCNNumber(tok string) (float64, string, bool) {
	m := reNumWord.FindString(tok)
	if m == "" {
		return 0, "", false
	}
	unit := strings.TrimPrefix(tok, m)
	mag, ok := cnToFloat(m)
	if !ok {
		return 0, "", false
	}
	return mag, unit, true
}

// cnToFloat 把中文/阿拉伯数词串转为 float。仅覆盖预筛所需的常见形态。
func cnToFloat(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	if f, ok := parseArabic(s); ok {
		return f, true
	}
	digit := map[rune]float64{
		'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5,
		'六': 6, '七': 7, '八': 8, '九': 9, '半': 0.5,
	}
	mult := map[rune]float64{
		'十': 10, '百': 100, '千': 1000, '万': 10000, '亿': 1e8,
	}
	var total, section, num float64
	hasNum := false
	for _, r := range s {
		if d, ok := digit[r]; ok {
			num = num*10 + d
			hasNum = true
		} else if m, ok := mult[r]; ok {
			if !hasNum {
				num = 1
			}
			section += num * m
			num = 0
			hasNum = false
		} else {
			return 0, false
		}
	}
	total = section + num
	if !hasNum && section == 0 {
		return 0, false
	}
	return total, true
}

// parseArabic 解析纯阿拉伯数字串（含小数）。
func parseArabic(s string) (float64, bool) {
	var f float64
	var frac float64
	var fscale float64 = 1
	dot := false
	seen := false
	for _, r := range s {
		if r == '.' {
			if dot {
				return 0, false
			}
			dot = true
			seen = true
			continue
		}
		if r < '0' || r > '9' {
			return 0, false
		}
		d := float64(r - '0')
		if dot {
			fscale *= 10
			frac = frac*fscale + d
		} else {
			f = f*10 + d
		}
		seen = true
	}
	if !seen {
		return 0, false
	}
	return f + frac/fscale, true
}

// runPatrolPrescreen 执行预筛：抽候选 → 向量聚类 → 跨章候选簇。任何异常都降级为 Ran=false，不阻断巡检。
func (s *Service) runPatrolPrescreen(ctx context.Context, novelID int64, chapters []chapter.Chapter) PrescreenReport {
	emb := patrolEmbedderProvider()
	if emb == nil {
		return PrescreenReport{Ran: false, Reason: "向量 embedder 未就绪，跳过预筛"}
	}
	if len(chapters) == 0 {
		return PrescreenReport{Ran: false, Reason: "无章节正文，跳过预筛"}
	}
	cands := s.extractPrescreenCands(ctx, novelID, chapters)
	if len(cands) < patrolPrescreenMinCluster {
		return PrescreenReport{Ran: false, Reason: "候选陈述不足，跳过预筛"}
	}
	clusters, err := clusterPrescreenCands(ctx, emb, cands)
	if err != nil {
		s.logger.Warn("一致性巡检预筛：聚类失败，跳过", "novel_id", novelID, "err", err)
		return PrescreenReport{Ran: false, Reason: "预筛聚类异常，已跳过"}
	}
	if len(clusters) == 0 {
		return PrescreenReport{Ran: false, Reason: "未聚类出跨章候选"}
	}
	return PrescreenReport{Ran: true, Clusters: clusters}
}

// formatPrescreen 把预筛报告渲染为喂给归并步的提示词片段；跳过时返回空串。
func formatPrescreen(r PrescreenReport) string {
	if !r.Ran || len(r.Clusters) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n### 预筛候选矛盾点（向量聚类，供重点核对，不替代你的判断）\n")
	sb.WriteString("以下由后端向量预筛得到：跨章出现、语义高度相近、且数值/事实可能分歧的陈述簇。" +
		"请在做全书归并时优先核对这些点，确认是否为真实张力；无则忽略。\n")
	for i, c := range r.Clusters {
		tag := ""
		if c.Divergent {
			tag = "【数值分歧】"
		}
		fmt.Fprintf(&sb, "\n#### 候选%d %s（相似度 %.2f，跨 %d 章）\n", i+1, tag, c.Sim, countChapters(c.Cands))
		for _, cand := range c.Cands {
			fmt.Fprintf(&sb, "- 第%d章：%s\n", cand.ReadingNumber, cand.Sentence)
		}
	}
	return sb.String()
}

// countChapters 统计簇内涉及的去重章数。
func countChapters(cands []PrescreenCand) int {
	seen := map[int]bool{}
	for _, c := range cands {
		seen[c.ReadingNumber] = true
	}
	return len(seen)
}

// dedupeStrings 去重并保持首次出现顺序。
func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
