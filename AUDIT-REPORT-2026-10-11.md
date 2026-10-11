# NovelGO 代码审计报告（2026-10-11）

> 范围：整仓库（Go 后端 `internal/`+`app/` + 前端 `frontend/src/`）  
> 侧重点：性能与流畅度、UI/UX 视觉与交互、代码质量/可维护性  
> 方法：4 个探查代理分头深读源码 + 交叉核查，所有发现均带 `文件:行号`  
> 注：本报告为只读审计，未修改任何文件。

---

## 一、总览（一句话）

状态管理/zustand 分层、错误基础设施设计、并发处理是**亮点**；真正的债务集中在三处：
1. **少数巨型函数/组件**（`applyAgentEvent` 556 行、三个 Graph 组件 ~80% 复制、多个 >800 行视图）；
2. **性能热点**——聊天流式渲染 O(n²) 重解析、重型依赖（g6/monaco/markdown+katex）全量进主包、后端 agent 主循环每轮全量 BPE 重算 + 搜索 20+ 冗余查询；
3. **后端架构级债**——`App` 上帝对象 + 150 行构造、包级全局单例双真相源、错误契约（`apperr`）建好却 95% 未落地。

---

## 二、P0（高影响，建议优先处理）

### 前端
- **F-1 聊天流式渲染 O(n²) + 无分段 memo**  
  `components/chat/ChatPanel.tsx:627-928` 每个 agent 事件都 `setTurns(prev => prev.map(...))` 重建整个数组；`MessageBubble.tsx:21` 的 `Markdown` 对每个 token 增长的全文**全量重解析**（react-markdown + remark-gfm + katex）。长回复主线程卡顿。  
  → 流式中「正在生成」气泡先渲染轻量纯文本 + 200ms 防抖/`rAF` 节流再交 `Markdown`；按 `turn.id/seg.id` 抽 `React.memo` 子段；完成后切回完整 Markdown。可复用现有 `EVENT_REORDER_TIMEOUT` 节流手法。

- **F-2 重型依赖全量进主包，无代码分割**  
  `vite.config.ts:6-25` 无 `manualChunks`；`CharacterListView.tsx:6`/`LocationListView.tsx:6`/`ArcListView.tsx:9` 静态 import `@antv/g6`；`ContentPanel.tsx:9` 静态 import `@monaco-editor/react`；`Markdown.tsx:12-20` 静态 import `react-markdown`+`katex`。首屏体积爆炸、TTI 高。  
  → 三个 Graph 用 `React.lazy`+`Suspense`；Editor/Markdown 按需懒加载；`vite.config.ts` 加 `manualChunks` 拆 g6/monaco/markdown+katex/react-query。

- **F-3 Monaco 走 jsDelivr CDN，离线不可用**  
  `@monaco-editor/react` 默认 `loader` 从 CDN 拉核心。桌面（Wails）常离线 → 编辑器空白/转圈。  
  → `loader.config({ monaco })` 指向本地 `monaco-editor` 包。

### 后端
- **B-1 `App` 上帝对象 + 150 行 `initWithConfig`**  
  `app/handler.go:47-101`（~35 字段）、`app/handler.go:276-434`。handler 直接读 `a.chapter`/`a.agent`，无法单测单域。  
  → 引入 `Container`/`Services` 装配层；`initWithConfig` 拆 `initStorage/initDomainStores/initLLM/initMCP/initRAG`（返回 error）；`App` 只留生命周期。

- **B-2 包级全局单例 + 配置双真相源**  
  `rag/embedder_onnx.go:34`、`rag/refresh_queue.go:42`、`rag/vector_store.go:345`（各 `sync.Once` 单例）；`config/config.go:18-21` 全局 + `config.Get()` 仅 `git/repo.go:52` 消费——与注入的 `a.cfg` 两份真相。  
  → 收进 `rag.Resources` 由 `initWithConfig` 注入；废弃 `config.Get()`，`git.New` 显式收参。

- **B-3 错误契约基础设施建好却 95% 未落地**  
  `internal/apperr/apperr.go` 设计精良（`Coder`+`CodeFromError` 穿透 wrap、零值降级），但仅 `app/skill_api.go` 3 处用，其余 ~95% handler 返裸 `error`（`chat.go:42`/`novel.go`/`chapter.go`…）。前端无法稳定区分错误类别。  
  → 决策并推进：核心写操作 handler 迁移到 `*apperr.Result[T]`（优先 novel/chapter/character）；给 `git.GitError`/`storage.OpError` 补 `Code()`。

- **B-4 主对话/LLM 流式缺少整体超时，provider 假死会永久阻塞 session**  
  `app/chat.go:49` ctx 无 `time.After`/`WithTimeout`；`internal/llm/stream.go:45-56` `http.Client.Timeout=0`、仅首字节超时。provider 首字节后挂起 → `agent.Run` 的 `streamLoop`（`agent/agent.go:243`）永远阻塞，只能杀进程。  
  → 主对话 ctx 加 10–15min 上限，超时走 `EventError` 优雅结束；LLM 层给 body 加读超时。

---

## 三、P1（重要，下一迭代解决）

### 前端
- **F-4 三个 Graph 组件 ~80% 复制粘贴**  
  `CharacterGraph.tsx:496` / `LocationGraph.tsx:442` / `StoryArcGraph.tsx:827` 同构（nodeId/safeJson/useMemo/初始化 useEffect/G6 behaviors/ResizeObserver/清理/工具栏/图例/loading-error 全重复）。  
  → 抽通用 `EntityGraphView`（props 注入 id 前缀、node/edge 样式工厂、命中解析、详情 render-prop、i18n key），三组件退化为数据适配层。

- **F-5 数据密集视图无虚拟滚动**  
  `BookshelfView.tsx:130`、`CharacterList.tsx:259`、`TimelineList.tsx:86`、`ChatPanel.tsx:1363-1518` 全量渲染。几百上千条后卡顿。  
  → 引入 `@tanstack/react-virtual`（项目已用 TanStack Query，风格一致）；章节侧栏已有手动窗口化（`SidebarChapterGroup.tsx:48`）可保留。

- **F-6 G6 图每次数据变化全量销毁重建**  
  `CharacterGraph.tsx:219-298` 依赖 `characters` 变化即 `graph.destroy()`+`new Graph()`+`render()`，丢失平移/缩放状态。  
  → 首次创建后仅 `setData()`+`render()` 增量更新。

- **F-7 错误色/图色硬编码，绕过 design token**  
  `text-red-500/600` 散落 `ChatPanel.tsx:1342,1471`、`ExportDialog.tsx:79`、`NovelEditDialog.tsx:100,109` 等（组件已用 `danger-*` token，仅文字色漏）；`graphColors.ts:3-32` 死值 hex（主边 `blue-500` 而非 `primary`）。  
  → 全部改 `text-destructive`；图色改读 CSS 变量/绑定 `primary`。

- **F-8 a11y 缺口**  
  可点 `div` 缺 `role/keyboard`（`TimelineList.tsx:90`、`CharacterGraph` 节点等）；封面上传按钮无 `aria-label`（`BookshelfView.tsx:146`）；聊天消息区无 `aria-live`。

- **F-9 状态组件不统一**  
  loading/empty/error 各视图写法各异（有的带 retry、有的无），缺共享 `StatusState` 组件。

### 后端
- **B-5 核心/安全包零单测**  
  `internal/rollback/`、`internal/agentcfg/`、`internal/approval/` 0 测试——直接决定「AI 会不会写坏小说」「对话质量」「安全边界」。  
  → 补表级单测（参考 `character/store_test.go` 内存 sqlite 模式）。

- **B-6 Store 暴露公共 `DB` 破坏封装**  
  `location/store.go:16`、`chapter/store.go` 等导出 `DB *gorm.DB`；`mcp_tools/*` 直接裸 gorm CRUD，绕过校验/埋点/删除守卫（`deletion.Guard`）。  
  → `DB` 改小写；store 仅暴露业务方法；通用写走受控接口。

- **B-7 `InitEmbedder` 失败即永久失败、不可重试**  
  `rag/embedder_onnx.go:56-76` `initOnce.Do` 失败 `initOnce` 已 fire，点击「重试」仍跳过 → 向量检索永久不可用。  
  → 用可重置状态机替代 `sync.Once`，仅成功后才置「已初始化」。

- **B-8 一次全局搜索触发 20+ 冗余 DB 查询**  
  `search/service.go:63-323` `SearchAll` 内各路重复 `GetReadingNumbersByNovel`+9 次 `ListByNovel`（各含 `COUNT(*)`+`JOIN`），`SearchByNovel`（`chapter/store.go:384-405`）又 Pluck 一次章节 id，`searchContent`/`searchRAG` 又各 `ListAllByNovel` 一次。≈22 次往返，SQLite `MaxOpenConns(1)` 下串行明显。  
  → 入口只取一次章节列表+阅读编号映射，注入三路；`SearchByNovel` 不再重复 Pluck。

- **B-9 消息 token 未持久化，每轮全量重 BPE**  
  `agent/agent.go:590-616` `CountMessageTokens` 结果未写 `TokenCount`（`session/types.go:60` 有列）；`chat.go:154` 每轮 `InitRunningTokens` 对整段历史重编码（`llm/token_counter.go:85-107`）→ agent 主循环稳定 CPU 热点。  
  → `appendMsg` 写 `TokenCount`，回放直接累加而非重编码。

- **B-10 搜索内容缓存一次读整本进内存 + 每次整体拷贝**  
  `search/service.go:445-477` `ensureContentCache` 循环 `git.ReadFile` 读全部章节正文到 map；`searchContent` 每次 RWMutex 下整体拷贝。长篇小说首次搜索占数百 MB 且同步阻塞。  
  → 懒加载 + LRU/容量上限；仅在启用正文精确搜索时构建；拷贝改只读快照或 `sync.Map`。

- **B-11 git 每 turn 多次 spawn + 多次 commit**  
  `git/repo.go:533` `runCmd`→`exec.Command`；`commitAIChanges`/`doCommit`（`chat.go:478-517`）每 turn 开头+结尾各 commit，编辑 N 章触发 ~3N 进程。  
  → 合并 `git commit -a -m`、去掉多余 `rev-parse`；批量编辑只提交一次。

- **B-12 操作日志回调放大 DB 负担**  
  `storage/operation_log.go:168-204` 每次 UPDATE/DELETE 先 `fetchOldRow`(SELECT)+INSERT 整行 JSON；agent 高频写入下串行放大。  
  → 只记变更字段；低关键小实体评估是否必记；日志改事务内批量/异步。

- **B-13 前端事件名魔法字符串作为跨语言契约未集中**  
  `app/chat.go:160/202/244/366`、`app/import_novel.go:39`、`rw_tools.go:176/186` 等裸字面量（部分已常量）。改名易漏改前后端不一致。  
  → 集中声明 `const EventChatStarted = "chat:started"` 全仓引用。

---

## 四、P2（一般，可渐进）

### 前端
- 阴影/圆角局部混用（`shadow-xs/sm/md/lg`、`rounded-xl/lg/2xl/md`），统一到 `index.css` 的 `--radius-*`/`--shadow-*`（P2-13）。
- `safeJson` 在 4 文件重复定义（`CharacterGraph.tsx:23` 等）→ 抽 `utils/safeJson.ts`（P2-1 后端同 P1-2）。
- 渲染循环内反复 `JSON.parse`（`CharacterListView.tsx:478`）→ react-query select 阶段预解析。
- 约 30+ 处裸 `console.error` → 统一 `logError(context, err)`（P2-2）。
- 7 处 `eslint-disable-next-line react-hooks/exhaustive-deps` → 审查/用 `useLatestRef` 消除。
- ContentPanel 对 Monaco 私有字段 `as any`（`_searchDecorations`）→ 隔离到 `monacoHighlighter.ts`。

### 后端
- 14 个 store CRUD 样板重复 → 泛型 `BaseRepo[T]`（`storage.PageResult[T]` 已存在）（P2-1）。
- `json.Marshal` 错误被忽略（`mcp_tools/base.go:391/407`、`agent/agent.go:663`）→ 至少 `logger.Warn`。
- 异步初始化 goroutine 无健康检查暴露（`app/handler.go:389-424`）→ 向前端暴露 RAG 就绪状态。
- `config.Get()` 近死代码 + `git.New` 隐式依赖全局（P2-4）。
- 模型资源文件名硬编码（`"vocab.txt"`/`"model.onnx"`）→ 收为具名常量（P2-5）。
- `Agent.Run` 主循环 675 行 → 抽流处理/工具循环/中断判定子状态机（P2-6）。
- 全局 `agent` 单例无并发 Run 保护（P2-10）；LLM 调用无全局并发信号量/429 冷却（P2-11）；session 搜索 leading-wildcard `LIKE` 全表扫描（P2-12）；hot path 序列化可换 jsoniter（P2-13）。

---

## 五、做得好的地方（务必保留）

- **前端状态分层克制**：全局 store 不缓存服务端响应，一律走 react-query；无任何 `@ts-ignore`/`@ts-nocheck`。
- **统一 react-query 范式**：各领域 useXxx+mutation+onSuccess 失效缓存，一致性高。
- **mermaid 懒加载+200ms 防抖**（`Markdown.tsx:96-157`）、**Markdown 安全管线**（`rehype-raw`→`rehype-sanitize`）、**章节列表手动窗口化**（`SidebarChapterGroup.tsx:48`）。
- **错误基础设施设计精良**（`apperr.go` 穿透 wrap+零值降级）。
- **并发处理审慎**：`CancelManager`、`settingsSnapshot` 写时复制、`startupMu` 临界区、每工具 recover panic、`Registry` `DisallowUnknownFields`。
- **启动把重活放后台**、`WAL+MaxOpenConns(1)` 正确、实体表普遍 `novel_id` 索引、搜索三路并发+vec0 KNN、Embedder 单例+批量推理+线程按核收缩。
- 生产源码无 `fmt.Println` 调试残留，日志统一 `slog`。

---

## 六、建议实施顺序（分阶段）

**阶段 1 — 立竿见影、低风险（建议先做）**
1. 前端 F-1 聊天流式节流/memo（每次对话都触达，收益最大）
2. 后端 B-4 主对话超时 + F-3 Monaco 本地化（离线可用性硬伤）
3. 后端 B-9 持久化 `token_count`、B-3 向量检索去掉未用 `embedding` 列、B-6 缓存 `ToAPIFormat`
4. 前端 F-7 token 一致性 + F-9 统一状态组件

**阶段 2 — 体积与性能主战役**
5. F-2 代码分割 + manualChunks（g6/monaco/markdown+katex）
6. 后端 B-8 搜索查询合并、B-10 内容缓存懒加载、B-11 git 合并、B-12 日志缩小
7. 前端 F-4 Graph 抽取、F-6 G6 增量更新、F-5 虚拟滚动

**阶段 3 — 架构级重构（独立分支 + 现有测试护栏）**
8. 后端 B-1 `App` 拆 Container、B-2 去全局单例、B-3 `apperr` 落地、B-7 `InitEmbedder` 可重试、B-13 事件名常量化
9. 后端 B-5 补 rollback/agentcfg/approval 单测
10. 前端 P0-1(`applyAgentEvent` 拆纯函数)+P0-3(ContentPanel helper) 并行；P1 巨型组件拆分

**阶段 4 — 打磨**
11. P2 各项（阴影圆角统一、safeJson 抽取、console.error→logError、eslint-disable 审查、a11y 补全、jsoniter、并发保护）

> 关键提示：阶段 1 全部为「不改动 schema、低风险、单点高收益」项，可立即开工；阶段 3 属结构性改造，务必在专门分支 + 现有 `internal/e2e/*` 与 `app/*_test.go` 回归护栏下进行。
