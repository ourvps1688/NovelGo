//go:build cgo && eval

// reranker_eval_test.go —— Reranker（P0-1）离线收益评测。
//
// 不依赖线上数据、不改动任何功能与写作流程，纯本地合成语料 + 真实检索/精排路径。
// 目的：量化 reranker 相对「纯余弦」与「余弦+MMR」在 Recall@K / MRR 上的真实增益，
// 用于判断 P0-1 是正向收益还是负优化（或仅持平）。
//
// 运行：
//   go test -tags eval -run TestRerankerOfflineEval ./internal/rag/ -v
//
// 默认模型目录 build/runtime/models（含 model.onnx + reranker.onnx），可用
// 环境变量 RAG_EVAL_MODELS 覆盖；ONNX 动态库默认 build/runtime，可用
// RAG_EVAL_RUNTIME 覆盖。

package rag

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	ort "github.com/yalue/onnxruntime_go"

	"github.com/novelgo/novelgo/internal/platform"
)

// ── 评测语料 ──────────────────────────────────────────────
// 合成中文小说语料：每章带明确实体与「植入事实」，部分植入事实刻意放在长段落中段
// （靠近 ~420 token 切点）以制造跨块边界难点；另设共享高频词（剑/家族/秘密/试炼）
// 的干扰章节，考验区分度——这正是 reranker 应当发力的场景。

type chapterSpec struct {
	id      int64
	title   string
	content string
}

type querySpec struct {
	q      string // 检索问句
	anchor string // 语料中唯一子串，用于定位 ground-truth chunk
	hard   bool   // true=复述/语义改写（更难），false=含实体原词（较易）
}

var evalChapters = []chapterSpec{
	{
		id:      101,
		title:   "云澜城",
		content: "陆昭自小在云澜城长大，城中人人皆知陆家那盏青铜古灯。青铜古灯唯有家主可点亮，灯芯燃起时，整座城池的灵气都会向陆府汇聚。陆昭十六岁那年，父亲陆衍突然失踪，青铜古灯也随之熄灭。云澜城的百姓说，灯灭之日，便是陆家气数将尽之时。陆昭不信命，他背着父亲留下的残破剑鞘，走出了云澜城的东门。东门外是一片望不到边的芦苇荡，风一过，白茫茫的芦花便如雪般翻涌。陆昭在芦苇荡里走了三天三夜，终于在一座废弃的渡口遇见了卖茶的老人。老人告诉他，青铜古灯的秘密不在灯里，而在灯下那方早已被尘封的青石砚台之中。陆昭记得，父亲临终前确实紧紧攥着一方青石砚台，只是那时他太小，只当那是寻常的镇纸。如今想来，那砚台上的云纹，竟与青铜古灯灯座的花纹一模一样。",
	},
	{
		id:      102,
		title:   "寒霜剑与苏璃",
		content: "苏璃是寒霜剑的当代执剑人，她的试炼在霜降那一日开始。寒霜剑出鞘时，方圆十丈的草木皆结白霜，连飞过的雀鸟也会瞬间凝在半空。苏璃的师父曾说，寒霜剑最忌心乱，一旦执剑人动了嗔念，剑气便会反噬其主。那一年，苏璃的师兄为了夺取寒霜剑，在试炼台上暗下毒手，苏璃虽胜，却也断了一根肋骨。她将断骨接好，提剑立誓，此生再不信任同门。多年以后，陆昭背着残破剑鞘来到寒霜剑宗，苏璃第一次在陌生人眼中看到了与寒霜剑同源的凛冽。她没有问陆昭的来历，只是把一碗温茶推到了他面前。陆昭不知道，这碗茶里，苏璃悄悄放了三粒能解百毒的寒霜莲子，那是寒霜剑宗从不外传的保命之物。",
	},
	{
		id:      103,
		title:   "南宫家的秘典",
		content: "南宫家族世代守护一部秘典，秘典的最后一页写着一句话：真正的秘密，从来不在书里。南宫家的长子南宫逸为了参透这话，把自己关在藏书阁整整七年。第七年的大雪夜，他终于发现，秘典的每一页边缘都暗藏着另一种文字，那文字记述的并非功法，而是一个家族百年来的背叛与救赎。南宫逸的叔父南宫烈，正是当年出卖南宫家基业的人，而南宫逸的父亲，则用半生隐忍换回了家族的安稳。南宫逸走出藏书阁时，已是满头白发，他烧掉了秘典，只带走了那句写在最后一页的话。从此南宫家再无秘典，却也再无人能借秘典之名行窃。陆昭后来在云澜城外遇见南宫逸，两人对坐饮茶，谁也没有提起过去，只是默契地笑了一下。",
	},
	{
		id:      104,
		title:   "古墓星图",
		content: "沈墨是个盗墓的，他这辈子进过最深的墓，是北境那座没有名字的古墓。古墓的穹顶绘着一幅星图，星图中央是一颗被九条锁链缠住的赤红之星。沈墨认得那星图的纹路，与陆昭剑鞘上的云纹竟有几分相似。他在古墓里待了九天九夜，临摹下整幅星图，却在离开时被墓中的机关削去了左手两根手指。沈墨没有后悔，他说，能用两根手指换一幅星图，这买卖划算。回到中原后，沈墨把星图卖给了南宫家，南宫逸看了一眼便知，那赤红之星，正是百年前被南宫家封印的某种东西的记号。陆昭在市集上撞见独臂的沈墨时，沈墨正用剩下的三根手指给人算命，卦象写的是：星落处，灯重燃。",
	},
	{
		id:      105,
		title:   "重燃的灯",
		content: "所有的线索，最终都汇向云澜城那盏熄灭的青铜古灯。陆昭、苏璃、南宫逸、沈墨，四个本不相干的人，因一盏灯和一幅星图聚到了一起。陆昭取出父亲留下的青石砚台，苏璃以寒霜莲子为引，南宫逸念出秘典末页的口诀，沈墨则摊开了那幅用两根手指换来的星图。当砚台嵌入灯座，星图覆上灯罩，寒霜莲子的露水渗入灯芯，青铜古灯竟真的重新燃起。灯芯亮起的刹那，云澜城上空的灵气如潮水般回流，陆衍当年封印在灯中的那个东西，也终于缓缓睁开了眼睛。陆昭终于明白，父亲并非失踪，而是把自己化作了灯中的守炉人，只为等这一日，等儿子带着砚台、星图与同伴归来，把灯重新点亮。",
	},
}

// evalQueries：每个 anchor 必须是某章 content 的唯一子串（评测会校验）。
var evalQueries = []querySpec{
	// —— 易：含实体原词 ——
	{q: "青铜古灯只有谁可以点亮", anchor: "青铜古灯唯有家主可点亮", hard: false},
	{q: "陆昭的父亲叫什么名字", anchor: "父亲陆衍突然失踪", hard: false},
	{q: "寒霜剑出鞘时有什么异象", anchor: "方圆十丈的草木皆结白霜", hard: false},
	{q: "苏璃的师兄做了什么", anchor: "师兄为了夺取寒霜剑", hard: false},
	{q: "秘典最后一页写了什么", anchor: "真正的秘密，从来不在书里", hard: false},
	{q: "南宫逸把自己关了多久", anchor: "关在藏书阁整整七年", hard: false},
	{q: "沈墨失去了几根手指", anchor: "削去了左手两根手指", hard: false},
	{q: "星图中央是什么", anchor: "九条锁链缠住的赤红之星", hard: false},
	{q: "陆昭用砚台做了什么", anchor: "砚台嵌入灯座", hard: false},
	{q: "陆衍把自己变成了什么", anchor: "化作了灯中的守炉人", hard: false},
	// —— 难：复述/语义改写，不含实体原词 ——
	{q: "云澜城百姓说灯灭意味着什么", anchor: "陆家气数将尽之时", hard: true},
	{q: "苏璃为何再不信任同门", anchor: "断了一根肋骨", hard: true},
	{q: "南宫家为何再无秘典", anchor: "烧掉了秘典", hard: true},
	{q: "沈墨卖星图给谁了", anchor: "卖给了南宫家", hard: true},
	{q: "四个人因什么聚到一起", anchor: "因一盏灯和一幅星图聚到了一起", hard: true},
	{q: "寒霜莲子被谁悄悄放进茶里", anchor: "放了三粒能解百毒的寒霜莲子", hard: true},
}

// ── 启动：加载 ONNX + 建库 ───────────────────────────────

// repoRoot 从当前工作目录向上查找 go.mod，定位仓库根（go test 的 cwd 是包目录，
// 不能直接用相对路径指向 build/runtime）。
func repoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func resolveOnnxLib() string {
	libName := func() string {
		switch runtime.GOOS {
		case "windows":
			return "onnxruntime.dll"
		case "darwin":
			return "libonnxruntime.dylib"
		default:
			return "libonnxruntime.so"
		}
	}()
	if v := os.Getenv("RAG_EVAL_RUNTIME"); v != "" {
		return filepath.Join(v, libName)
	}
	root := repoRoot()
	cands := []string{
		filepath.Join("build", "runtime", libName),
		filepath.Join("build", "bin", "runtime", libName),
	}
	if root != "" {
		cands = append(cands,
			filepath.Join(root, "build", "runtime", libName),
			filepath.Join(root, "build", "bin", "runtime", libName),
		)
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	if p, err := platform.ResolveOnnxLib(); err == nil {
		return p
	}
	return ""
}

func modelsDir() string {
	if v := os.Getenv("RAG_EVAL_MODELS"); v != "" {
		return v
	}
	root := repoRoot()
	if root != "" {
		return filepath.Join(root, "build", "runtime", "models")
	}
	return "build/runtime/models"
}

var rerankerAvailable bool

func TestMain(m *testing.M) {
	lib := resolveOnnxLib()
	if lib == "" {
		fmt.Fprintln(os.Stderr, "FATAL: 找不到 ONNX Runtime 动态库（设置 RAG_EVAL_RUNTIME 或确保在 build/runtime 下）")
		os.Exit(1)
	}
	ort.SetSharedLibraryPath(lib)

	md := modelsDir()
	ragLog := slog.Default()
	InitEmbedder(md, ragLog)
	InitReranker(md, ragLog)

	if _, err := GetEmbedder(); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: embedder 初始化失败: %v\n", err)
		os.Exit(1)
	}
	if _, err := GetReranker(); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: reranker 不可用（将只跑 cosine / MMR 两档）: %v\n", err)
		rerankerAvailable = false
	} else {
		rerankerAvailable = true
	}
	os.Exit(m.Run())
}

// ── 指标 ──────────────────────────────────────────────

type metric struct {
	hits  int     // 命中次数（ground-truth 落入 topK）
	rrSum float64 // reciprocal rank 之和
	n     int     // 查询总数
}

func (m *metric) add(hit bool, rank int) {
	m.n++
	if hit {
		m.hits++
	}
	if rank > 0 {
		m.rrSum += 1.0 / float64(rank)
	}
}

func (m *metric) recall() float64 {
	if m.n == 0 {
		return 0
	}
	return float64(m.hits) / float64(m.n)
}

func (m *metric) mrr() float64 {
	if m.n == 0 {
		return 0
	}
	return m.rrSum / float64(m.n)
}

// ── 主评测 ──────────────────────────────────────────────

func TestRerankerOfflineEval(t *testing.T) {
	ctx := context.Background()
	md := modelsDir()

	tok, err := NewTokenizer(filepath.Join(md, "vocab.txt"))
	if err != nil {
		t.Fatalf("加载 tokenizer 失败: %v", err)
	}
	emb, err := GetEmbedder()
	if err != nil {
		t.Fatalf("embedder 未就绪: %v", err)
	}

	// 建一个带 vec0 扩展的临时 SQLite。
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), "rag-eval.db")
	sqlDB, err := sql.Open("sqlite3", "file:"+dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("打开评测数据库失败: %v", err)
	}
	defer sqlDB.Close()

	store := NewVectorStore(sqlDB, emb, slog.Default())

	const topKPool = 20 // 粗排候选池
	ks := []int{1, 3, 5, 10}
	overlaps := []int{0, 50, 100}

	// arm 指标：key = overlap -> k -> arm
	type armMetrics map[string]*metric
	agg := map[int]map[int]armMetrics{} // overlap -> k -> arm -> metric
	newArm := func() armMetrics {
		return armMetrics{"cosine": {}, "mmr": {}, "rerank": {}}
	}
	for _, ov := range overlaps {
		agg[ov] = map[int]armMetrics{}
		for _, k := range ks {
			agg[ov][k] = newArm()
		}
	}

	// 分难度聚合（仅在 K=5 汇总，便于解读）。
	easyM := map[string]*metric{"cosine": {}, "mmr": {}, "rerank": {}}
	hardM := map[string]*metric{"cosine": {}, "mmr": {}, "rerank": {}}

	for _, ov := range overlaps {
		novelID := ov + 1000 // 每个 overlap 配置用一个独立 novel，避免 vec 表冲突
		// 构建 + 索引 chunks（按当前 overlap）。
		var chunks []Chunk
		for _, ch := range evalChapters {
			texts, positions := SplitText(ch.content, DefaultChunkSize, ov, tok)
			for i, txt := range texts {
				chunks = append(chunks, Chunk{
					ID:           fmt.Sprintf("%d_%d", ch.id, i),
					Content:      txt,
					ChapterID:    ch.id,
					ChunkType:    "content",
					ChunkIndex:   i,
					StartRunePos: positions[i],
					Metadata:     map[string]any{"chapter_id": ch.id},
				})
			}
		}
		if err := store.IndexChunks(ctx, int64(novelID), chunks); err != nil {
			t.Fatalf("索引 chunks 失败 (overlap=%d): %v", ov, err)
		}

		// 预计算 ground-truth：每个 anchor 落在哪些 chunk（按子串匹配）。
		truth := map[string][]string{} // anchor -> chunkIDs
		for _, q := range evalQueries {
			var ids []string
			for _, c := range chunks {
				if strings.Contains(c.Content, q.anchor) {
					ids = append(ids, c.ID)
				}
			}
			if len(ids) == 0 {
				t.Fatalf("anchor 未在任何 chunk 中找到（语料/切分漂移）: %q", q.anchor)
			}
			truth[q.anchor] = ids
		}

		for _, q := range evalQueries {
			cands, err := store.Search(ctx, int64(novelID), q.q, topKPool, nil)
			if err != nil {
				t.Fatalf("检索失败: %v", err)
			}
			truthIDs := truth[q.anchor]

			// 三档排序结果。
			cosine := cands // SQL 已按 distance 升序
			mmr := MMRRerank(q.q, cands, ks[len(ks)-1], 0.7)
			var rerank []SearchResult
			if rerankerAvailable {
				rerank = Rerank(ctx, q.q, cands, ks[len(ks)-1])
			}

			rankOf := func(results []SearchResult) int {
				for i, r := range results {
					for _, id := range truthIDs {
						if r.ChunkID == id {
							return i + 1
						}
					}
				}
				return 0
			}
			hitIn := func(results []SearchResult, k int) bool {
				return rankOf(results[:min(k, len(results))]) > 0
			}

			arms := map[string][]SearchResult{"cosine": cosine, "mmr": mmr}
			if rerankerAvailable {
				arms["rerank"] = rerank
			}
			for arm, results := range arms {
				for _, k := range ks {
					agg[ov][k][arm].add(hitIn(results, k), rankOf(results[:min(k, len(results))]))
				}
				// 难度聚合（K=5）。
				if q.hard {
					hardM[arm].add(hitIn(results, 5), rankOf(results[:min(5, len(results))]))
				} else {
					easyM[arm].add(hitIn(results, 5), rankOf(results[:min(5, len(results))]))
				}
			}
		}
	}

	// ── 输出报告 ──
	fmt.Println("\n==================== Reranker 离线收益评测 ====================")
	fmt.Printf("reranker 可用: %v（模型 bge-reranker-base）\n", rerankerAvailable)
	easyCount, hardCount := 0, 0
	for _, q := range evalQueries {
		if q.hard {
			hardCount++
		} else {
			easyCount++
		}
	}
	fmt.Printf("语料: %d 章 / %d 条查询（易 %d, 难 %d）\n", len(evalChapters), len(evalQueries), easyCount, hardCount)
	fmt.Println()

	header := func() {
		fmt.Printf("%-7s %-4s | %-8s %-8s %-8s | %-8s %-8s %-8s\n",
			"ovlp", "K", "cosine", "mmr", "rerank", "cosR@K", "mmrR@K", "rrR@K")
		fmt.Printf("%-7s %-4s-+-%-8s-%-8s-%-8s-+-%-8s-%-8s-%-8s\n",
			"-----", "--", "--------", "--------", "--------", "--------", "--------", "--------")
	}
	header()
	for _, ov := range overlaps {
		for _, k := range ks {
			mc, mm, mr := agg[ov][k]["cosine"], agg[ov][k]["mmr"], agg[ov][k]["rerank"]
			rr := "  N/A  "
			if rerankerAvailable {
				rr = fmt.Sprintf("%.3f", mr.recall())
			}
			fmt.Printf("%-7d %-4d | %-8s %-8s %-8s | %-8s %-8s %-8s\n",
				ov, k,
				fmt.Sprintf("%.3f", mc.recall()), fmt.Sprintf("%.3f", mm.recall()), rr,
				fmt.Sprintf("%.3f", mc.mrr()), fmt.Sprintf("%.3f", mm.mrr()),
				func() string {
					if rerankerAvailable {
						return fmt.Sprintf("%.3f", mr.mrr())
					}
					return "  N/A  "
				}())
		}
	}

	fmt.Println("\n── 分难度（K=5, Recall@5 / MRR）──")
	fmt.Printf("%-6s | %-22s %-22s %-22s\n", "难度", "cosine", "mmr", "rerank")
	row := func(name string, m map[string]*metric) {
		c, m2 := m["cosine"], m["mmr"]
		r := "N/A"
		if rerankerAvailable {
			r = fmt.Sprintf("%.3f/%.3f", m["rerank"].recall(), m["rerank"].mrr())
		}
		fmt.Printf("%-6s | %-22s %-22s %-22s\n", name,
			fmt.Sprintf("%.3f/%.3f", c.recall(), c.mrr()),
			fmt.Sprintf("%.3f/%.3f", m2.recall(), m2.mrr()),
			r)
	}
	row("易", easyM)
	row("难", hardM)

	// ── 结论性提示 ──
	fmt.Println("\n── 解读 ──")
	if rerankerAvailable {
		for _, ov := range overlaps {
			mc, mr := agg[ov][5]["cosine"], agg[ov][5]["rerank"]
			delta := mr.recall() - mc.recall()
			sign := "+"
			if delta < 0 {
				sign = ""
			}
			fmt.Printf("  overlap=%d: reranker 相对 cosine 的 Recall@5 增量 = %s%.3f (MRR %.3f→%.3f)\n",
				ov, sign, delta, mc.mrr(), mr.mrr())
		}
	} else {
		fmt.Println("  reranker 不可用，无法量化其增益；当前生产路径为 cosine+MMR。")
	}
	fmt.Println("=================================================================")
}
