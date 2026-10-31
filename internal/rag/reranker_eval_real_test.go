//go:build cgo && eval

// reranker_eval_real_test.go —— Reranker（P0-1）真实数据收益复核。
//
// 与 reranker_eval_test.go（合成语料）互补：本测试直接读取生产数据库
// build/bin/novel-agent.db 中已有的向量索引（vec_novel_<id> 表的 content 列），
// 用生产 IndexChunks 路径在「临时可写库」重建索引（embedding 是纯函数，与生产线
// 逐条等价），然后构造真实章节语料的检索查询，走真实 Search + Rerank / MMRRerank
// 路径，对比「纯余弦 / 余弦+MMR / reranker」三档在 Recall@K 与 MRR 上的差异。
//
// 关键约束（守住隐私与功能红线）：
//   - 仅以 mode=ro 读取生产 DB 的向量表，绝不写入、绝不修改、绝不删除任何生产数据；
//   - 向量重算只在临时库进行（向量是纯函数，与生产线逐条相等），不触碰生产 DB；
//   - 全程本地运行，章节正文不出本机，不上传任何外部服务。
//
// 运行：
//   go test -tags eval -run TestRerankerRealDataEval ./internal/rag/ -v
//
// 可用环境变量覆盖生产 DB 路径：RAG_EVAL_REAL_DB。

package rag

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

// ── 真实 DB 加载 ──────────────────────────────────────────

func realDBPath(t *testing.T) string {
	if v := os.Getenv("RAG_EVAL_REAL_DB"); v != "" {
		return v
	}
	root := repoRoot()
	if root == "" {
		t.Fatalf("找不到仓库根（需在 novelgo 仓库内运行）；或设置 RAG_EVAL_REAL_DB 指向 novel-agent.db")
	}
	return filepath.Join(root, "build", "bin", "novel-agent.db")
}

func listVecTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'vec_novel_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func vecTableNameToID(name string) int64 {
	s := strings.TrimPrefix(name, "vec_novel_")
	id, _ := strconv.ParseInt(s, 10, 64)
	return id
}

// pickLargestVecTable 选 chunk 数最多的 1 张 vec 表（控规模，取最具代表性的密集语料）。
func pickLargestVecTable(db *sql.DB, tables []string) (string, error) {
	best := ""
	bestN := -1
	for _, tbl := range tables {
		var n int
		if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl)).Scan(&n); err != nil {
			return "", fmt.Errorf("统计 %s 失败: %w", tbl, err)
		}
		if n > bestN {
			bestN = n
			best = tbl
		}
	}
	return best, nil
}

type realChunk struct {
	chunkID   string
	content   string
	chunkType string
	chapterID int64
}

// readRealChunks 只读 vec0 表的 content 等 metadata 列（不带 MATCH 的全表扫描）。
func readRealChunks(db *sql.DB, table string) ([]realChunk, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT chunk_id, content, chunk_type, chapter_id FROM %s", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []realChunk
	for rows.Next() {
		var c realChunk
		var chID sql.NullInt64
		if err := rows.Scan(&c.chunkID, &c.content, &c.chunkType, &chID); err != nil {
			return nil, err
		}
		if chID.Valid {
			c.chapterID = chID.Int64
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ── query 构造 ──────────────────────────────────────────

type realQuery struct {
	text  string
	kind  string // "single"=单块中部子句（gt=源块） | "cross"=相邻块跨边界拼接（gt=双块）
	gtIDs []string
}

func lastRunes(r []rune, n int) string {
	if len(r) <= n {
		return string(r)
	}
	return string(r[len(r)-n:])
}

func firstRunes(r []rune, n int) string {
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}

// pickUniform 从 [0,n) 均匀挑 k 个下标。
func pickUniform(n, k int) []int {
	if n <= k {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	out := make([]int, k)
	step := float64(n) / float64(k)
	for i := 0; i < k; i++ {
		out[i] = int(float64(i) * step)
	}
	return out
}

// buildRealQueries 仅基于 content 类 chunk 构造两类 query（summary/brief 仅作候选干扰）。
//   - single：取 content chunk 中部约 70 字子串，ground truth = 该 chunk（片段召回自身）。
//   - cross：取相邻 content chunk 的尾 35 + 头 35 拼接，ground truth = 这两块（跨边界召回）。
func buildRealQueries(raw []realChunk) []realQuery {
	byChap := map[int64][]realChunk{}
	var order []int64
	for _, c := range raw {
		if c.chunkType != "content" {
			continue
		}
		if utf8.RuneCountInString(c.content) < 120 {
			continue // 过短块，子串检索无意义
		}
		if _, ok := byChap[c.chapterID]; !ok {
			order = append(order, c.chapterID)
		}
		byChap[c.chapterID] = append(byChap[c.chapterID], c)
	}

	const singleCap, crossCap = 200, 100
	var qs []realQuery
	for _, ch := range order {
		cs := byChap[ch]
		// 单块：均匀挑 2 个。
		for _, idx := range pickUniform(len(cs), 2) {
			if len(qs) >= singleCap {
				break
			}
			c := cs[idx]
			runes := []rune(c.content)
			segLen := 70
			if len(runes) < segLen+40 {
				segLen = len(runes) - 40
			}
			if segLen < 40 {
				continue
			}
			start := len(runes) / 3
			if start+segLen > len(runes) {
				start = len(runes) - segLen
			}
			seg := strings.TrimSpace(string(runes[start : start+segLen]))
			if utf8.RuneCountInString(seg) < 30 {
				continue
			}
			qs = append(qs, realQuery{text: seg, kind: "single", gtIDs: []string{c.chunkID}})
		}
		// 跨块：若有 ≥2 块，挑中部一对相邻。
		if len(cs) >= 2 && len(qs) < singleCap+crossCap {
			i := len(cs) / 2
			a, b := cs[i], cs[i+1]
			q := lastRunes([]rune(a.content), 35) + firstRunes([]rune(b.content), 35)
			qs = append(qs, realQuery{text: q, kind: "cross", gtIDs: []string{a.chunkID, b.chunkID}})
		}
		if len(qs) >= singleCap+crossCap {
			break
		}
	}
	return qs
}

// ── 主评测 ──────────────────────────────────────────────

func TestRerankerRealDataEval(t *testing.T) {
	ctx := context.Background()
	dbPath := realDBPath(t)

	sqlite_vec.Auto()
	realDB, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro&_busy_timeout=8000")
	if err != nil {
		t.Fatalf("打开真实数据库失败: %v", err)
	}
	defer realDB.Close()

	vecTables, err := listVecTables(realDB)
	if err != nil {
		t.Fatalf("枚举 vec 表失败: %v", err)
	}
	if len(vecTables) == 0 {
		t.Fatalf("未在真实 DB 中找到任何 vec_novel_%% 表（可能尚未建立向量索引）")
	}

	chosen, err := pickLargestVecTable(realDB, vecTables)
	if err != nil {
		t.Fatalf("选择 vec 表失败: %v", err)
	}
	novelID := vecTableNameToID(chosen)

	raw, err := readRealChunks(realDB, chosen)
	if err != nil {
		t.Fatalf("读取真实 chunks 失败: %v（若 vec0 不支持无约束全表扫描，请反馈以改用 rowid 分页）", err)
	}
	if len(raw) == 0 {
		t.Fatalf("vec 表 %s 为空", chosen)
	}

	typeDist := map[string]int{}
	for _, c := range raw {
		typeDist[c.chunkType]++
	}
	t.Logf("选用 vec 表 %s（novel_id=%d），共 %d 条 chunk", chosen, novelID, len(raw))
	for k, v := range typeDist {
		t.Logf("  chunk_type=%s: %d", k, v)
	}

	emb, err := GetEmbedder()
	if err != nil {
		t.Fatalf("embedder 未就绪: %v", err)
	}

	// 临时可写库：用生产 IndexChunks 重建索引（向量纯函数 = 生产线逐条等价）。
	tmpPath := filepath.Join(t.TempDir(), "real-eval.db")
	tmpDB, err := sql.Open("sqlite3", "file:"+tmpPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	defer tmpDB.Close()

	store := NewVectorStore(tmpDB, emb, slog.Default())
	var chunks []Chunk
	for _, c := range raw {
		chunks = append(chunks, Chunk{
			ID:        c.chunkID,
			Content:   c.content,
			ChapterID: c.chapterID,
			ChunkType: c.chunkType,
		})
	}
	idxStart := time.Now()
	if err := store.IndexChunks(ctx, novelID, chunks); err != nil {
		t.Fatalf("重建真实索引失败: %v", err)
	}
	t.Logf("索引重建完成：%d 块，耗时 %s", len(chunks), time.Since(idxStart))

	queries := buildRealQueries(raw)
	var nSingle, nCross int
	for _, q := range queries {
		if q.kind == "cross" {
			nCross++
		} else {
			nSingle++
		}
	}
	t.Logf("构造查询 %d 条（单块 %d，跨块 %d）", len(queries), nSingle, nCross)

	// ── 三档评测 ──
	topKPool := 20
	ks := []int{1, 3, 5, 10}
	newArm := func() map[string]*metric {
		return map[string]*metric{"cosine": {}, "mmr": {}, "rerank": {}}
	}
	agg := newArm()       // 整体
	aggSingle := newArm() // 单块类
	aggCross := newArm()  // 跨块类

	var rerankTotalMs int64
	var rerankCalls int

	for _, q := range queries {
		cands, err := store.Search(ctx, novelID, q.text, topKPool, nil)
		if err != nil {
			t.Fatalf("检索失败: %v", err)
		}
		truth := q.gtIDs

		cosine := cands // SQL 已按 distance 升序
		mmr := MMRRerank(q.text, cands, topKPool, 0.7)
		var rerank []SearchResult
		if rerankerAvailable {
			t0 := time.Now()
			rerank = Rerank(ctx, q.text, cands, topKPool)
			rerankTotalMs += time.Since(t0).Milliseconds()
			rerankCalls++
		}

		rankOf := func(rs []SearchResult) int {
			for i, r := range rs {
				for _, id := range truth {
					if r.ChunkID == id {
						return i + 1
					}
				}
			}
			return 0
		}
		hitIn := func(rs []SearchResult, k int) bool {
			return rankOf(rs[:min(k, len(rs))]) > 0
		}

		arms := map[string][]SearchResult{"cosine": cosine, "mmr": mmr}
		if rerankerAvailable {
			arms["rerank"] = rerank
		}
		target := agg
		if q.kind == "cross" {
			target = aggCross
		} else {
			target = aggSingle
		}
		for arm, rs := range arms {
			for _, k := range ks {
				agg[arm].add(hitIn(rs, k), rankOf(rs[:min(k, len(rs))]))
				target[arm].add(hitIn(rs, k), rankOf(rs[:min(k, len(rs))]))
			}
		}
	}

	// ── 输出报告 ──
	fmt.Println("\n================ Reranker 真实数据收益复核 ================")
	fmt.Printf("数据源: 生产 DB %s 的 %s（novel_id=%d）\n", filepath.Base(dbPath), chosen, novelID)
	fmt.Printf("chunk 总数: %d（content/summary/brief 混合入索引，仅 content 构造 query）\n", len(raw))
	fmt.Printf("查询: %d（单块 %d，跨块 %d），topK 候选池=%d\n", len(queries), nSingle, nCross, topKPool)
	fmt.Printf("reranker 可用: %v（模型 bge-reranker-base）\n", rerankerAvailable)
	if rerankerAvailable && rerankCalls > 0 {
		fmt.Printf("reranker 平均单 query 精排延迟: %d ms（共 %d 次调用）\n", rerankTotalMs/int64(rerankCalls), rerankCalls)
	}
	fmt.Println()

	reportBlock := func(title string, m map[string]*metric) {
		fmt.Printf("── %s（Recall@K / MRR）──\n", title)
		fmt.Printf("%-7s | %-8s %-8s %-8s | %-8s %-8s %-8s\n", "K", "cosine", "mmr", "rerank", "cosMRR", "mmrMRR", "rrMRR")
		for _, k := range ks {
			mc, mm, mr := m["cosine"], m["mmr"], m["rerank"]
			rr := "  N/A  "
			if rerankerAvailable {
				rr = fmt.Sprintf("%.3f", mr.mrr())
			}
			fmt.Printf("%-7d | %-8s %-8s %-8s | %-8s %-8s %-8s\n",
				k,
				fmt.Sprintf("%.3f", mc.recall()), fmt.Sprintf("%.3f", mm.recall()), func() string {
					if rerankerAvailable {
						return fmt.Sprintf("%.3f", mr.recall())
					}
					return "  N/A  "
				}(),
				fmt.Sprintf("%.3f", mc.mrr()), fmt.Sprintf("%.3f", mm.mrr()), rr)
		}
	}
	reportBlock("整体", agg)
	reportBlock("单块子句类", aggSingle)
	reportBlock("跨块边界类", aggCross)

	fmt.Println("\n── 结论性解读 ──")
	if rerankerAvailable {
		for _, label := range []string{"整体", "单块子句类", "跨块边界类"} {
			var m map[string]*metric
			switch label {
			case "整体":
				m = agg
			case "单块子句类":
				m = aggSingle
			default:
				m = aggCross
			}
			mc, mr := m["cosine"], m["rerank"]
			fmt.Printf("  %s: reranker 相对 cosine 的 MRR 增量 = %+.3f（R@5 %.3f→%.3f）\n",
				label, mr.mrr()-mc.mrr(), mc.recall(), mr.recall())
		}
	}
	fmt.Println("================================================================")
}
