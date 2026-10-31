package search

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/preference"
	"github.com/novelgo/novelgo/internal/rag"
	"github.com/novelgo/novelgo/internal/reader"
	"github.com/novelgo/novelgo/internal/setting"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/storyarc"
	"github.com/novelgo/novelgo/internal/timeline"
)

// Service 协调全局搜索：实体 LIKE + 正文精确 + RAG 语义。
// 缓存章节正文在内存中，避免重复读文件。
type Service struct {
	logger       *slog.Logger
	charStore    *character.Store
	locStore     *location.Store
	tlStore      *timeline.Store
	arcStore     *storyarc.Store
	chapStore    *chapter.Store
	readerStore  *reader.Store
	prefStore    *preference.Store
	settingStore *setting.Store
	vectorStore  *rag.VectorStore

	mu    sync.RWMutex
	cache map[int64]map[int64]string // novelID → chapterID → content
}

// NewService 创建搜索服务。
func NewService(logger *slog.Logger, charStore *character.Store, locStore *location.Store,
	tlStore *timeline.Store, arcStore *storyarc.Store, chapStore *chapter.Store,
	readerStore *reader.Store, prefStore *preference.Store, settingStore *setting.Store,
	vecStore *rag.VectorStore) *Service {
	return &Service{
		logger:       logger,
		charStore:    charStore,
		locStore:     locStore,
		tlStore:      tlStore,
		arcStore:     arcStore,
		chapStore:    chapStore,
		readerStore:  readerStore,
		prefStore:    prefStore,
		settingStore: settingStore,
		vectorStore:  vecStore,
		cache:        make(map[int64]map[int64]string),
	}
}

// SearchAll 执行全局搜索，并发执行三类搜索后合并结果。
func (s *Service) SearchAll(ctx context.Context, novelID int64, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// 阶段2 B-8：入口只取一次章节列表 + 阅读编号映射，注入三路搜索，
	// 消除 searchContent / searchRAG 各自再全表扫描一次、以及 SearchByNovel 内部重复 Pluck。
	allChapters, err := s.chapStore.ListAllByNovel(ctx, nil, novelID)
	if err != nil {
		s.logger.Warn("search: 获取章节列表失败", "err", err)
		allChapters = nil
	}
	readingNumbers, err := s.chapStore.GetReadingNumbersByNovel(ctx, nil, novelID)
	if err != nil {
		s.logger.Warn("search: 获取阅读编号失败", "err", err)
		readingNumbers = nil
	}

	var (
		wg             sync.WaitGroup
		entityResults  []Result
		contentResults []Result
		ragResults     []Result
	)

	wg.Add(3)
	go func() { defer wg.Done(); entityResults = s.searchEntities(ctx, novelID, query, readingNumbers) }()
	go func() { defer wg.Done(); contentResults = s.searchContent(ctx, novelID, query, allChapters) }()
	go func() { defer wg.Done(); ragResults = s.searchRAG(ctx, novelID, query, allChapters) }()
	wg.Wait()

	var results []Result
	results = append(results, entityResults...)
	results = append(results, contentResults...)
	results = append(results, ragResults...)

	return results, nil
}

// searchEntities 在各实体 store 上执行 LIKE 搜索。
// readingNumbers 由 SearchAll 统一获取后注入，避免重复查询。
func (s *Service) searchEntities(ctx context.Context, novelID int64, query string, readingNumbers map[int64]int) []Result {
	var results []Result
	if readingNumbers == nil {
		// 兜底（仅 SearchAll 注入失败时）：单独查询一次。
		rn, err := s.chapStore.GetReadingNumbersByNovel(ctx, nil, novelID)
		if err != nil {
			s.logger.Warn("chapter list for entity search failed", "err", err)
		} else {
			readingNumbers = rn
		}
	}

	// 人物
	chars, err := s.charStore.ListByNovel(ctx, novelID, character.ListByNovelOptions{
		Search:     query,
		PageParams: storage.PageParams{Page: 1, Size: EntityLimit},
		Order:      "updated_at DESC", // 显式锚定默认排序，避免依赖 store 隐式默认
	})
	if err != nil {
		s.logger.Warn("character search failed", "err", err)
	} else if chars != nil {
		for _, c := range chars.Items {
			results = append(results, Result{
				Type:    "character",
				ID:      c.ID,
				Title:   c.Name,
				PanelID: "characters",
			})
		}
	}

	// 地点
	locs, err := s.locStore.ListByNovel(ctx, novelID, location.ListByNovelOptions{
		Search:     query,
		PageParams: storage.PageParams{Page: 1, Size: EntityLimit},
		Order:      "name ASC", // 显式锚定默认排序，避免依赖 store 隐式默认
	})
	if err != nil {
		s.logger.Warn("location search failed", "err", err)
	} else if locs != nil {
		for _, l := range locs.Items {
			results = append(results, Result{
				Type:     "location",
				ID:       l.ID,
				Title:    l.Name,
				Subtitle: l.LocationType,
				PanelID:  "locations",
			})
		}
	}

	// 时间线
	tlResult, err := s.tlStore.ListByNovel(ctx, novelID, timeline.ListByNovelOptions{
		Search:     query,
		PageParams: storage.PageParams{Page: 1, Size: EntityLimit},
		Order:      "importance DESC", // 保持原 SearchByNovel 排序
	})
	if err != nil {
		s.logger.Warn("timeline search failed", "err", err)
	} else if tlResult != nil {
		for _, e := range tlResult.Items {
			results = append(results, Result{
				Type:          "timeline",
				ID:            e.ID,
				Title:         e.Title,
				Subtitle:      e.Category, // 英文原值，前端 t("timeline."+subtitle) 翻译
				ReadingNumber: e.TargetReadingNumber,
				PanelID:       "timeline",
			})
		}
	}

	// 故事弧
	arcsResult, err := s.arcStore.ListByNovel(ctx, novelID, storyarc.ListByNovelOptions{
		Search:     query,
		PageParams: storage.PageParams{Page: 1, Size: EntityLimit},
		Order:      "importance DESC, created_at ASC", // 显式锚定默认排序，避免依赖 store 隐式默认
	})
	if err != nil {
		s.logger.Warn("story arc search failed", "err", err)
	} else if arcsResult != nil {
		for _, arc := range arcsResult.Items {
			results = append(results, Result{
				Type:     "storyarc",
				ID:       arc.ID,
				Title:    arc.Name,
				Subtitle: arc.ArcType,
				PanelID:  "storyarcs",
			})
		}
	}

	// 弧线节点（4b: 新增 node 搜索分支，独立 Type="arc_node"）
	nodesResult, err := s.arcStore.ListNodesByNovel(ctx, novelID, storyarc.ListNodesOptions{
		Search:     query,
		PageParams: storage.PageParams{Page: 1, Size: EntityLimit},
		Order:      "story_arc_id, target_reading_number ASC, id ASC", // 显式锚定 + 补 story_arc_id 前缀，避免同弧线节点被打散
	})
	if err != nil {
		s.logger.Warn("arc node search failed", "err", err)
	} else if nodesResult != nil {
		// 构建 arcID→name 映射，用于显示所属弧线名称（先从已搜到的 arcs 取，未命中不额外查询）
		// nil 守卫：arc 搜索失败时 arcsResult 为 nil，此处降级为空 map（subtitle 留空，不 panic）。
		arcNameMap := make(map[int64]string)
		if arcsResult != nil {
			for _, arc := range arcsResult.Items {
				arcNameMap[arc.ID] = arc.Name
			}
		}
		for _, node := range nodesResult.Items {
			subtitle := arcNameMap[node.StoryArcID]
			results = append(results, Result{
				Type:          "arc_node",
				ID:            node.ID,
				Title:         node.Title,
				Subtitle:      subtitle,
				ReadingNumber: node.TargetReadingNumber,
				PanelID:       "storyarcs",
			})
		}
	}

	// 读者视角（4b: 新增 reader 搜索分支，subtitle 传英文 type 原值，前端 t("reader."+subtitle) 翻译）
	readersResult, err := s.readerStore.ListByNovel(ctx, novelID, reader.ListByNovelOptions{
		Search:     query,
		PageParams: storage.PageParams{Page: 1, Size: EntityLimit},
		Order:      "type, planted_chapter_id ASC", // 显式锚定默认排序，避免依赖 store 隐式默认
	})
	if err != nil {
		s.logger.Warn("reader search failed", "err", err)
	} else if readersResult != nil {
		for _, r := range readersResult.Items {
			title := r.Content
			if runes := []rune(title); len(runes) > 40 {
				title = string(runes[:40]) + "…"
			}
			var chapterID int64
			if r.PlantedChapterID != nil {
				chapterID = *r.PlantedChapterID
			}
			readingNumber := readingNumbers[chapterID]
			results = append(results, Result{
				Type:          "reader",
				ID:            r.ID,
				Title:         title,
				Subtitle:      r.Type, // 英文原值，前端 t("reader."+subtitle) 翻译
				ChapterID:     chapterID,
				ReadingNumber: readingNumber,
				PanelID:       "reader",
			})
		}
	}

	// 偏好（4b: 新增 preference 搜索分支，调 global + novel 两次合并，subtitle=category 自由文本原样显示）
	prefOpts := preference.ListOptions{
		Search:     query,
		PageParams: storage.PageParams{Page: 1, Size: EntityLimit},
		Order:      "created_at ASC", // 显式锚定默认排序，避免依赖 store 隐式默认
	}
	globalPrefs, gErr := s.prefStore.ListGlobalPreferences(ctx, prefOpts)
	if gErr != nil {
		s.logger.Warn("global preference search failed", "err", gErr)
		globalPrefs = nil
	}
	novelPrefs, nErr := s.prefStore.ListNovelPreferences(ctx, novelID, prefOpts)
	if nErr != nil {
		s.logger.Warn("novel preference search failed", "err", nErr)
		novelPrefs = nil
	}
	var prefItems []preference.PreferenceItem
	if globalPrefs != nil {
		prefItems = append(prefItems, globalPrefs.Items...)
	}
	if novelPrefs != nil {
		prefItems = append(prefItems, novelPrefs.Items...)
	}
	for _, pref := range prefItems {
		title := pref.Content
		if runes := []rune(title); len(runes) > 40 {
			title = string(runes[:40]) + "…"
		}
		results = append(results, Result{
			Type:     "preference",
			ID:       pref.ID,
			Title:    title,
			Subtitle: pref.Category, // 自由文本，原样显示
			PanelID:  "preferences",
		})
	}

	// 设定（4b: 新增 setting 搜索分支，subtitle=category 自由文本原样显示）
	settingsResult, sErr := s.settingStore.ListByNovel(ctx, novelID, setting.ListOptions{
		Search:     query,
		PageParams: storage.PageParams{Page: 1, Size: EntityLimit},
		Order:      "created_at ASC", // 显式锚定默认排序，避免依赖 store 隐式默认
	})
	if sErr != nil {
		s.logger.Warn("setting search failed", "err", sErr)
		settingsResult = nil
	}
	if settingsResult != nil {
		for _, st := range settingsResult.Items {
			title := st.Content
			if runes := []rune(title); len(runes) > 40 {
				title = string(runes[:40]) + "…"
			}
			results = append(results, Result{
				Type:     "setting",
				ID:       st.ID,
				Title:    title,
				Subtitle: st.Category, // 自由文本，原样显示
				PanelID:  "novel-settings",
			})
		}
	}

	// 章节
	chapters, err := s.chapStore.SearchByNovel(ctx, nil, novelID, query, EntityLimit, readingNumbers)
	if err != nil {
		s.logger.Warn("chapter title search failed", "err", err)
	} else {
		for _, ch := range chapters {
			results = append(results, Result{
				Type:          "chapter",
				ID:            ch.ID,
				Title:         ch.Title,
				Subtitle:      "titleMatch", // 4b: i18n key 后缀，前端 t("chapter."+subtitle) 翻译
				ChapterID:     ch.ID,
				ReadingNumber: ch.ReadingNumber,
				FilePath:      ch.FilePath,
				PanelID:       "chapters",
			})
		}
	}

	return results
}

// searchContent 在章节正文中做精确字符串匹配，使用内存缓存避免重复读文件。
// allChapters 由 SearchAll 统一查询后注入，避免二次全表扫描（阶段2 B-8）。
// 正文按需懒加载（阶段2 B-10），仅在命中章节上保留内容，内存占用受 maxCachedChaptersPerNovel 限制。
func (s *Service) searchContent(ctx context.Context, novelID int64, query string, allChapters []chapter.Chapter) []Result {
	if len(allChapters) == 0 {
		// 兜底：未注入章节列表时再查一次（极少路径）。
		var err error
		allChapters, err = s.chapStore.ListAllByNovel(ctx, nil, novelID)
		if err != nil {
			s.logger.Warn("search content: 获取章节列表失败，退化为仅搜已缓存章节", "err", err)
			allChapters = nil
		}
	}

	type match struct {
		chapterID int64
		position  int // 命中字符偏移（按 rune 计）
	}
	var matches []match
	// 仅缓存命中章节的正文（命中数受 ContentLimit 约束，内存可控），避免整本驻留。
	matchedContent := make(map[int64]string)

	// 迭代目标：正常路径遍历注入/回退查到的章节；DB 不可用时退化为仅遍历已缓存章节。
	type chapterRef struct {
		id       int64
		filePath string
	}
	var refs []chapterRef
	if len(allChapters) == 0 {
		s.mu.RLock()
		for chID := range s.cache[novelID] {
			refs = append(refs, chapterRef{id: chID})
		}
		s.mu.RUnlock()
	} else {
		for _, ch := range allChapters {
			refs = append(refs, chapterRef{id: ch.ID, filePath: ch.FilePath})
		}
	}

	for _, ref := range refs {
		content, ok := s.loadChapterContent(novelID, ref.id, ref.filePath)
		if !ok {
			continue
		}
		pos := 0
		for {
			idx := strings.Index(content[pos:], query)
			if idx < 0 {
				break
			}
			absPos := utf8.RuneCountInString(content[:pos+idx])
			matches = append(matches, match{chapterID: ref.id, position: absPos})
			matchedContent[ref.id] = content
			pos = pos + idx + len(query)
			if len(matches) >= ContentLimit {
				break
			}
		}
		if len(matches) >= ContentLimit {
			break
		}
	}

	if len(matches) == 0 {
		return nil
	}

	chapMeta := make(map[int64]chapter.Chapter, len(allChapters))
	for _, ch := range allChapters {
		chapMeta[ch.ID] = ch
	}

	var results []Result
	for _, m := range matches {
		content, ok := matchedContent[m.chapterID]
		if !ok {
			content, ok = s.loadChapterContent(novelID, m.chapterID, chapMeta[m.chapterID].FilePath)
		}
		if !ok {
			continue
		}
		prefix, hit, suffix := buildContext(content, m.position, query)
		meta := chapMeta[m.chapterID]
		results = append(results, Result{
			Type:          "content",
			ID:            0,
			Title:         meta.Title,
			ChapterID:     m.chapterID,
			ReadingNumber: meta.ReadingNumber,
			FilePath:      git.ChapterPath(m.chapterID),
			MatchPrefix:   prefix,
			MatchHit:      hit,
			MatchSuffix:   suffix,
			MatchPosition: m.position,
			MatchLen:      utf8.RuneCountInString(query),
			Relevance:     1,
			PanelID:       "chapters",
		})
	}

	return results
}

// buildContext 提取命中位置前后 ContextRadius 个中文字符的上下文，返回前缀、命中词、后缀。
// 前端负责用 JSX 渲染高亮，避免 dangerouslySetInnerHTML。
func buildContext(content string, matchRunePos int, query string) (prefix, hit, suffix string) {
	runes := []rune(content)
	total := len(runes)
	qLen := utf8.RuneCountInString(query)

	start := matchRunePos - ContextRadius
	if start < 0 {
		start = 0
	}
	end := matchRunePos + qLen + ContextRadius
	if end > total {
		end = total
	}

	if start > 0 {
		prefix = "..."
	}
	prefix += string(runes[start:matchRunePos])
	hit = string(runes[matchRunePos : matchRunePos+qLen])
	suffix = string(runes[matchRunePos+qLen : end])
	if end < total {
		suffix += "..."
	}

	return prefix, hit, suffix
}

// loadChapterContent 返回指定章节的正文，按需懒加载并缓存（带容量上限）。
// 阶段2 B-10：取代原先“首次搜索即把整本读进内存”的 ensureContentCache，
// 避免长篇小说首次搜索占用数百 MB 且同步阻塞；仅缓存命中章节，超出上限不再缓存。
func (s *Service) loadChapterContent(novelID, chapterID int64, filePath string) (string, bool) {
	s.mu.RLock()
	if m, ok := s.cache[novelID]; ok {
		if c, ok2 := m[chapterID]; ok2 {
			s.mu.RUnlock()
			return c, true
		}
	}
	s.mu.RUnlock()

	content, err := git.ReadFile(novelID, filePath)
	if err != nil {
		return "", false
	}

	s.mu.Lock()
	m := s.cache[novelID]
	if m == nil {
		m = make(map[int64]string)
		s.cache[novelID] = m
	}
	// 容量上限：超出后仅本次搜索可用、不写缓存，避免无限增长。
	if len(m) < maxCachedChaptersPerNovel {
		m[chapterID] = content
	}
	s.mu.Unlock()

	return content, true
}

// UpdateCachedChapter 在章节保存后更新缓存中的对应章节内容。
func (s *Service) UpdateCachedChapter(novelID, chapterID int64, newContent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if chapMap, ok := s.cache[novelID]; ok {
		chapMap[chapterID] = newContent
	}
}

// DeleteCachedChapter 移除已删除章节的搜索缓存内容。
func (s *Service) DeleteCachedChapter(novelID, chapterID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if chapMap, ok := s.cache[novelID]; ok {
		delete(chapMap, chapterID)
	}
}

// searchRAG 执行向量语义搜索。
// allChapters 由 SearchAll 统一查询后注入，避免二次全表扫描（阶段2 B-8）。
func (s *Service) searchRAG(ctx context.Context, novelID int64, query string, allChapters []chapter.Chapter) []Result {
	if s.vectorStore == nil {
		return nil
	}

	fetchK := RagTopK * 2
	if fetchK > 40 {
		fetchK = 40
	}

	ragResults, err := s.vectorStore.Search(ctx, novelID, query, fetchK, nil)
	if err != nil {
		s.logger.Warn("搜索: RAG 检索失败", "novel_id", novelID, "err", err)
		return nil
	}

	filtered := make([]rag.SearchResult, 0, len(ragResults))
	for _, r := range ragResults {
		if r.Relevance >= 0.3 {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	reranked := rag.MMRRerank(query, filtered, RagTopK, 0.7)

	chapMeta := make(map[int64]chapter.Chapter, len(allChapters))
	for _, ch := range allChapters {
		chapMeta[ch.ID] = ch
	}

	var results []Result
	for _, r := range reranked {
		// vec 表只存 chapter_id，按 id 反查当前标题和展示编号。
		meta := chapMeta[r.ChapterID]
		contentPreview := r.Content
		runes := []rune(contentPreview)
		if len(runes) > 200 {
			contentPreview = string(runes[:200]) + "..."
		}

		results = append(results, Result{
			Type:          "rag",
			ID:            0,
			Title:         meta.Title,
			ChapterID:     r.ChapterID,
			ReadingNumber: meta.ReadingNumber,
			FilePath:      git.ChapterPath(meta.ID),
			MatchPrefix:   contentPreview,
			MatchLen:      utf8.RuneCountInString(r.Content),
			MatchPosition: r.StartRunePos,
			Relevance:     r.Relevance,
			PanelID:       "chapters",
		})
	}

	return results
}
