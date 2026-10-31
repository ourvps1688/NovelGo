package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/activity"
	"github.com/novelgo/novelgo/internal/agent"
	"github.com/novelgo/novelgo/internal/approval"
	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/commit"
	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/content"
	"github.com/novelgo/novelgo/internal/deletion"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/logger"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/mcpserver"
	"github.com/novelgo/novelgo/internal/migrate"
	"github.com/novelgo/novelgo/internal/novel"
	"github.com/novelgo/novelgo/internal/pipeline"
	"github.com/novelgo/novelgo/internal/preference"
	"github.com/novelgo/novelgo/internal/rag"
	"github.com/novelgo/novelgo/internal/reader"
	"github.com/novelgo/novelgo/internal/retcon"
	"github.com/novelgo/novelgo/internal/rollback"
	"github.com/novelgo/novelgo/internal/search"
	"github.com/novelgo/novelgo/internal/session"
	"github.com/novelgo/novelgo/internal/setting"
	"github.com/novelgo/novelgo/internal/skill"
	"github.com/novelgo/novelgo/internal/skill/remote"
	"github.com/novelgo/novelgo/internal/snapshot"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/storyarc"
	"github.com/novelgo/novelgo/internal/style"
	"github.com/novelgo/novelgo/internal/timeline"
	"github.com/novelgo/novelgo/internal/voice"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/novelgo/novelgo/internal/writing"
)

// App 是 Wails 绑定的根对象。前端通过 window.go.main.App 调用其导出方法。
// 各领域方法按文件拆分（novel.go / chapter.go 等），均接收 *App。
type App struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *slog.Logger

	// 启动状态：让前端能区分"未配置 / 初始化中 / 就绪 / 失败"，而不是只拿到一个
	// 布尔量。startupMu 同时保护 startupState 与 frontendReady —— 两者会交叉读取
	// （setStartupState 读 frontendReady 决定是否推送，FrontendReady 读
	// startupState 决定补发什么），必须放在同一临界区，否则可能出现双方都判定
	// "对方未就绪"而漏掉一次推送。详见 startup_state.go。
	startupMu     sync.Mutex
	startupState  StartupState
	frontendReady bool

	// 关窗确认：初始化期间拦截一次关闭，等前端弹窗回传用户选择，见 quit_confirm.go。
	// 与启动状态共用 startupMu：OnBeforeClose 需要在同一临界区里同时读到阶段与标记。
	quitConfirmPending bool
	quitConfirmed      bool

	cfg        *config.AppConfig
	settings   *config.AppSettings
	settingsMu sync.Mutex // 串行化 settings 的并发读写，见 config.go 的 updateSettings / settingsSnapshot
	db         *gorm.DB

	llmClient     *llm.Client
	agent         *agent.Agent
	cancelMgr     *agent.CancelManager
	registry      *mcp_tools.Registry
	mcpServer     *mcpserver.Server
	activeNovelMu sync.Mutex
	activeNovelID atomic.Int64
	approvals     *approval.Service
	vectorStore   atomic.Pointer[rag.VectorStore]
	searchService atomic.Pointer[search.Service]

	novel          *novel.Store
	preference     *preference.Store
	setting        *setting.Store
	chapter        *chapter.Store
	chapterService *chapter.Service
	contentService *content.Service
	commitService  *commit.Service
	character      *character.Store
	session        *session.Store
	skill          *skill.Store
	// remote 持有远程 skill 市场服务，用于 ListRemoteSkills / GetRemoteSkillContent / InstallRemoteSkill。
	remote     *remote.Service
	style      *style.Store
	timeline   *timeline.Store
	storyarc   *storyarc.Store
	location   *location.Store
	reader     *reader.Store
	turnCommit *rollback.Store
	writing    *writing.Store
	volume     *volume.Store
	snapshots  *snapshot.Service
	retcons    *retcon.Service
	activity   *activity.Store
	pipeline   *pipeline.Service
}

// New 创建 App 实例。初始化在 OnStartup 中完成。
func New(logger *slog.Logger) *App {
	return &App{
		logger: logger,
		startupState: StartupState{
			Phase:   PhaseInitializing,
			Version: 1,
		},
	}
}

// wailsEmitter 是 event.Emitter 的 wails runtime 实现：把领域事件经
// runtime.EventsEmit 推到前端。领域包（agent/pipeline/mcp_tools）只依赖
// event.Emitter 接口，由 app 在启动时注入本实现，避免它们直接 import wails。
type wailsEmitter struct{}

// Emit 实现 event.Emitter。
func (wailsEmitter) Emit(ctx context.Context, name string, data ...any) {
	runtime.EventsEmit(ctx, name, data...)
}

// ── 生命周期 ──────────────────────────────────────────────

// OnStartup 在 Wails 窗口创建后调用，完成基础设施初始化。
//
// 注意 Wails 是在独立 goroutine 中调用本函数的，因此初始化与前端加载并发：
// 前端很可能在迁移跑完之前就挂载完毕。此时它靠 startup:state 事件与
// GetStartupState 得知"正在初始化"，而不是靠 IsInitialized() 的布尔量——后者
// 会把"初始化中"和"初始化失败"都误判为"未配置"。
func (a *App) OnStartup(ctx context.Context) {
	a.ctx, a.cancel = context.WithCancel(ctx)

	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotInitialized) {
			// 首次启动，不自动初始化，等前端展示 InitView 再由用户手动触发
			a.setStartupState(PhaseUnconfigured, "")
			return
		}
		a.logger.Error("加载配置失败", "err", err)
		a.setStartupState(PhaseFailed, err.Error())
		return
	}
	_ = a.runInit(cfg)
}

// runInit 执行初始化并维护启动状态：先进入 initializing，成功置 ready，失败置
// failed 并带上原因。OnStartup / Initialize / RetryStartup 共用，保证三个入口
// 的状态语义一致。
func (a *App) runInit(cfg *config.AppConfig) error {
	if err := a.initWithConfig(cfg); err != nil {
		a.logger.Error("应用初始化失败", "err", err)
		a.setStartupState(PhaseFailed, err.Error())
		return err
	}
	a.setStartupState(PhaseReady, "")
	return nil
}

// reconcileOrphanChapterFiles 对所有小说执行孤儿章节文件回收（best-effort）。
// 依赖 a.chapterService 与 a.novel，均在 initWithConfig 中先于本调用就绪。
func (a *App) reconcileOrphanChapterFiles() error {
	novels, err := a.GetNovels()
	if err != nil {
		return fmt.Errorf("列出小说失败: %w", err)
	}
	for _, n := range novels {
		if _, err := a.chapterService.ReconcileOrphanFiles(a.ctx, n.ID); err != nil {
			a.logger.Warn("孤儿章节文件回收失败", "novel_id", n.ID, "err", err)
		}
	}
	return nil
}

// OnBeforeClose 在窗口关闭前调用，返回 true 表示阻止关闭。
//
// 初始化进行中（含数据库迁移）时劝阻退出：迁移本身可断点续跑，但用户往往
// 不知道自己在等什么，中途退出会白白浪费一次启动。这里选择"劝阻 + 允许"而非
// 硬阻止——强杀进程无法拦截，硬阻止只会把用户逼向更粗暴的退出方式。
//
// Wails v2 的 MessageDialog 在 Windows/Linux 会忽略自定义按钮（只给系统按钮，
// 返回值恒为 "Ok"/"OK"），无法表达"继续等待 / 退出"这组选项，因此改由前端渲染
// 弹窗：本函数只做判定与登记，用户的最终选择经 ConfirmQuit / CancelQuit 回来。
func (a *App) OnBeforeClose(ctx context.Context) bool {
	a.startupMu.Lock()
	decision := decideClose(a.startupState.Phase, a.frontendReady, a.quitConfirmPending, a.quitConfirmed)
	if decision == quitConfirm {
		a.quitConfirmPending = true
	}
	a.startupMu.Unlock()

	switch decision {
	case quitConfirm:
		a.logger.Info("初始化进行中，请求前端确认是否退出")
		runtime.EventsEmit(ctx, quitConfirmEventName)
		return true
	case quitForce:
		a.logger.Info("确认期间再次请求关闭窗口，直接退出")
		return false
	default:
		return false
	}
}

// OnShutdown 在 Wails 窗口关闭前调用，释放资源。
func (a *App) OnShutdown(shutdownCtx context.Context) {
	a.logger.Info("应用关闭，释放资源")

	// 1. 取消根上下文，通知所有运行中的 agent 停止
	if a.cancel != nil {
		a.cancel()
	}
	if a.mcpServer != nil {
		stopCtx, cancel := context.WithTimeout(shutdownCtx, 5*time.Second)
		if err := a.mcpServer.Stop(stopCtx); err != nil {
			a.logger.Error("停止本地 MCP server 失败", "err", err)
			// 在途调用可能尚未退出，保留它们的依赖，交由进程退出回收。
			cancel()
			return
		}
		cancel()
	}

	// 2. 停止 RAG 后台消费者
	if q := rag.GetRefreshQueue(); q != nil {
		q.Stop()
	}
	// 3. 释放 ONNX embedder（非阻塞，避免未初始化时死锁）
	if emb := rag.TryGetEmbedder(); emb != nil {
		_ = emb.Close()
	}

	// 4. 关闭数据库（放在最后，确保上述清理中的 DB 操作已完成）
	if a.db != nil {
		if err := storage.Close(a.db); err != nil {
			a.logger.Error("关闭数据库失败", "err", err)
		}
	}
}

// IsInitialized 报告核心运行时是否已就绪。
//
// 启动阶段的前端路由应使用 GetStartupState，而非这个兼容性布尔接口。
func (a *App) IsInitialized() bool {
	return a.cfg != nil
}

// Initialize 在用户触发首次初始化时调用：写入指针文件后执行初始化。
// dataDir 参数保留用于前端兼容，实际数据目录由平台决定。
func (a *App) Initialize(dataDir string) error {
	if err := a.beginInitialization(PhaseUnconfigured); err != nil {
		return err
	}
	if err := config.Save(dataDir); err != nil {
		saveErr := fmt.Errorf("保存配置失败: %w", err)
		a.setStartupState(PhaseUnconfigured, saveErr.Error())
		return saveErr
	}

	cfg, err := config.Load()
	if err != nil {
		// 回到 unconfigured 而非 failed：指针文件刚写入却读不回来，属于"配置还没
		// 建立起来"。留在设置页让用户重选目录，比进错误页更可恢复——错误页的重试
		// 会再读同一个文件，必然再次失败。
		loadErr := fmt.Errorf("加载配置失败: %w", err)
		a.setStartupState(PhaseUnconfigured, loadErr.Error())
		return loadErr
	}

	return a.runInit(cfg)
}

// RetryStartup 重新执行初始化，供前端在失败页点"重试"时调用。
//
// 与 Initialize 的区别：不写指针文件——配置已经存在，重试只是重新读取并初始化。
// 若复用 Initialize，会拿前端传来的路径覆盖用户已有配置。
func (a *App) RetryStartup() error {
	if err := a.beginInitialization(PhaseFailed); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotInitialized) {
			// 指针文件在运行期被删除：回到首次配置页。否则用户会停在错误页，而
			// 重试读的是同一个已不存在的文件，永远失败。
			loadErr := fmt.Errorf("加载配置失败: %w", err)
			a.setStartupState(PhaseUnconfigured, loadErr.Error())
			return loadErr
		}
		a.setStartupState(PhaseFailed, err.Error())
		return fmt.Errorf("加载配置失败: %w", err)
	}
	return a.runInit(cfg)
}

// initWithConfig 在配置加载成功后初始化所有运行时模块。
// 只有全部步骤成功才会将 a.cfg 设为非 nil，防止半初始化状态下 IsInitialized() 误报。
// 返回 error 让调用方（OnStartup/Initialize）能感知失败并通知前端。
func (a *App) initWithConfig(cfg *config.AppConfig) error {
	config.Set(cfg)

	// 1. 异步加载 ONNX 模型（不阻塞 GUI，尽早调用）
	rag.InitEmbedder(config.ModelsDir(), a.logger)

	// 2. 打开全局数据库
	db, err := storage.Open(config.GlobalDBPath(), a.logger)
	if err != nil {
		a.logger.Error("打开数据库失败", "err", err)
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	initSucceeded := false
	defer func() {
		if !initSucceeded {
			if closeErr := storage.Close(db); closeErr != nil {
				a.logger.Warn("关闭失败初始化的数据库连接失败", "err", closeErr)
			}
		}
	}()

	// 3. 自动建表
	if err := migrate.Run(db, a.logger); err != nil {
		a.logger.Error("数据库迁移失败", "err", err)
		return fmt.Errorf("数据库迁移失败: %w", err)
	}

	a.activity = activity.NewStore(db, a.logger)
	a.activity.InitTracking(a.ctx)

	// 4. 加载运行时配置
	settings, err := config.LoadSettings(db)
	if err != nil {
		a.logger.Error("加载设置失败", "err", err)
		return fmt.Errorf("加载设置失败: %w", err)
	}
	a.settings = settings

	// 5. 注册操作日志钩子（失败降级：回滚功能不可用，其余正常）
	if err := storage.RegisterOplogHooks(db); err != nil {
		a.logger.Error("注册操作日志钩子失败，回滚功能将不可用", "err", err)
	}
	// 5b. 注册台账 temporal/history 写路径钩子（WBS 2.6，失败降级：履历缺失，写入照常）
	if err := storage.RegisterLedgerTemporalHooks(db); err != nil {
		a.logger.Error("注册台账 temporal 钩子失败，变更履历将不可用", "err", err)
	}

	// 6. 创建所有领域 store
	a.novel = novel.NewStore(db, a.logger)
	a.preference = preference.NewStore(db, a.logger)
	a.setting = setting.NewStore(db, a.logger)
	a.chapter = chapter.NewStore(db, a.logger)
	a.character = character.NewStore(db, a.logger)
	a.session = session.NewStore(db, a.logger)
	a.timeline = timeline.NewStore(db, a.logger)
	a.storyarc = storyarc.NewStore(db, a.logger)
	a.location = location.NewStore(db, a.logger)
	a.reader = reader.NewStore(db, a.logger)
	a.turnCommit = rollback.NewStore(db, a.logger)
	a.commitService = commit.NewService(a.turnCommit, a.logger)
	a.writing = writing.NewStore(db, a.logger)
	a.volume = volume.NewStore(db, a.logger)

	// 6b. 章节创建回填钩子（R0 后补充闭环）：章节记录创建后按阅读序号把
	// 章纲任务卡的 chapter_id 绑定上（同事务原子生效）。失败只告警不影响建章。
	chapter.SetCreatedHook(func(ctx context.Context, tx *gorm.DB, novelID, chapterID int64, readingNumber int) {
		bound, err := a.timeline.BindChapterID(ctx, tx, novelID, chapterID, readingNumber)
		if err != nil {
			a.logger.Warn("章纲任务卡 chapter_id 回填失败（不影响章节创建）",
				"novel_id", novelID, "chapter_id", chapterID, "err", err)
			return
		}
		if bound {
			a.logger.Debug("章纲任务卡已回填 chapter_id",
				"novel_id", novelID, "chapter_id", chapterID, "reading_number", readingNumber)
		}
	})
	a.snapshots = snapshot.NewService(db, a.volume, a.chapter)
	a.retcons = retcon.NewService(retcon.NewStore(db))
	// retcon 受控编辑留痕（WBS 3.2）三执行体：回滚点固化=snapshot.Commit；差异摘要=
	// snapshot.DiffAgainstCurrent + FormatDelta；回滚=snapshot.Restore（raw SQL 还原，
	// 见 2.2）。Record/Rollback 终态前再 Commit 让既定事实注入源跟上 retcon 后 canon。
	a.retcons.SetCommitSnapshot(func(ctx context.Context, novelID, volumeID int64) (int64, error) {
		snap, err := a.snapshots.Commit(ctx, novelID, volumeID)
		if err != nil {
			return 0, err
		}
		return snap.ID, nil
	})
	a.retcons.SetDiffSummary(func(ctx context.Context, snapshotID int64) (string, error) {
		delta, err := a.snapshots.DiffAgainstCurrent(ctx, snapshotID)
		if err != nil {
			return "", err
		}
		return snapshot.FormatDelta(delta), nil
	})
	a.retcons.SetRestorer(func(ctx context.Context, _, snapshotID int64) error {
		_, err := a.snapshots.Restore(ctx, snapshotID)
		return err
	})
	// 风格案（kind=style，P2 T4.2）回滚只还原 voice_dimensions，事实层零触碰。
	a.retcons.SetStyleRestorer(func(ctx context.Context, _, snapshotID int64) error {
		_, err := a.snapshots.RestoreStyle(ctx, snapshotID)
		return err
	})
	chapterDeletionGuard := deletion.NewGuard(
		deletion.For(deletion.EntityChapter, a.timeline.ChapterDeletionBlockers),
		deletion.For(deletion.EntityChapter, a.storyarc.ChapterDeletionBlockers),
		deletion.For(deletion.EntityChapter, a.reader.ChapterDeletionBlockers),
		deletion.For(deletion.EntityChapter, a.character.ChapterDeletionBlockers),
	)
	a.chapterService = chapter.NewService(a.chapter, a.volume, chapterDeletionGuard, a.logger,
		func() chapter.ChapterChunkCleaner {
			vectorStore := a.vectorStore.Load()
			if vectorStore == nil {
				return nil
			}
			return vectorStore
		},
		func() chapter.ChapterCacheInvalidator {
			if svc := a.searchService.Load(); svc != nil {
				return svc
			}
			return nil
		},
	)
	// 启动期孤儿章节文件回收（E1 崩溃窗口兜底）：DB 已删但文件未删的残留文件。
	if err := a.reconcileOrphanChapterFiles(); err != nil {
		a.logger.Warn("启动期孤儿章节文件清理失败", "err", err)
	}
	// 内容派生编排服务：把 SaveContent 中 RAG/搜索缓存/字数统计/写作记录的跨介质
	// 协调从 app 绑定层下沉到领域层（分层合规，A1）。
	a.contentService = content.NewService(a.logger, a.chapter, a.writing,
		func() content.SearchUpdater {
			if svc := a.searchService.Load(); svc != nil {
				return svc
			}
			return nil
		})
	s, err := skill.NewStore(a.logger, config.UserSkillsDir())
	if err != nil {
		a.logger.Error("初始化 skill store 失败", "err", err)
	} else {
		a.skill = s
		// 初始化远程 skill 市场服务（基于 skillStore 和 logger）
		a.remote = remote.NewService(a.skill, a.logger)
	}

	// 7. 初始化 MCP 工具注册表
	a.registry = mcp_tools.NewRegistry(a.logger)
	mcp_tools.SetEmitter(wailsEmitter{})
	mcp_tools.RegisterAllTools(a.registry)
	a.mcpServer = mcpserver.New(a.registry, db, a.currentNovel, a.logger, []string{"get_chapter_list", "list_volumes"})

	// 8. 初始化 LLM 客户端
	userConfig, err := llm.LoadUserConfig(config.LLMConfigPath())
	if err != nil {
		a.logger.Warn("加载 LLM 配置失败，使用空配置", "err", err)
		userConfig = &llm.UserLLMConfig{}
	}
	providers := llm.Merge(llm.Builtin, userConfig)
	// 日志分类（排障噪声治理）：LLM 流式/工具调用/重试日志走独立分类日志器
	// （novelgo-llm.log），与应用日志（novelgo.log）分离，避免互相刷屏。
	a.llmClient = llm.NewClient(providers, logger.LLM())

	// 9. 初始化审批服务
	a.approvals = approval.NewService(a.logger, a.settings.ApprovalMode)

	// 10. 创建 Agent 实例（全局复用）
	a.cancelMgr = agent.NewCancelManager()
	a.agent = agent.New(a.llmClient, a.registry, a.session, a.chapter, db, a.approvals, a.logger, a.skill, a.cancelMgr, wailsEmitter{})

	// 10.5 初始化 style store（全局风格素材）
	a.style = style.NewStore(db, a.logger)
	a.db = db

	// 解析默认模型（"provider/model"），供 S2 蓝图阶段多轮 agent 使用。
	defaultProv, defaultModel := a.parseModelKey(a.settings.SelectedModelKey)
	a.pipeline = pipeline.NewService(db, a.agent, a.skill, a.llmClient, defaultProv, defaultModel, a.ctx, a.logger)
	// 注入流水线事件总线实现（领域包经 event.Emitter 接口发布，避免直接依赖 wails）。
	pipeline.SetEventBus(wailsEmitter{})
	// 注入 S0 对齐所需的 statesync 子 Agent 执行器：经由 agent.RunSubAgent 派发（mcp_tools 提供指令构造）。
	a.pipeline.SetStateSyncRunner(func(ctx context.Context, novelID, chapterID int64) error {
		return mcp_tools.RunStateSyncForChapter(ctx, func(c context.Context, req mcp_tools.SubAgentRequest) (string, error) {
			return a.agent.RunSubAgent(c, agent.RunOptions{}, req)
		}, a.logger, "pipeline-s0-align", novelID, chapterID)
	})
	// 注入章节 CI 检测执行器（R5-3 T1.2）：检测任务 chapter kind 经 chapterci 子 Agent
	// 跑 L1 增量检查并落质量档案（与写后章节 CI 同一执行体）。
	a.pipeline.SetChapterCIRunner(func(ctx context.Context, novelID, chapterID int64) error {
		return mcp_tools.RunChapterCIForChapter(ctx, func(c context.Context, req mcp_tools.SubAgentRequest) (string, error) {
			return a.agent.RunSubAgent(c, agent.RunOptions{}, req)
		}, a.skill, a.db, a.logger, "pipeline-detect", novelID, chapterID)
	})
	// 注入 G_freeze 自动冻结执行体（WBS 2.5）：卷过 S4+S5 收口 / 完本收口时由流水线自动
	// 置冻结并同事务提交 committed snapshot（Freeze 内含 2.4 冻结前台账审计闸门，幂等）。
	a.pipeline.SetVolumeFreezer(func(ctx context.Context, novelID, volumeID int64) error {
		_, _, err := a.snapshots.Freeze(ctx, novelID, volumeID)
		return err
	})
	// 注入「已冻结卷既定事实」提供者（WBS 2.3）：S4/S5/S6/章节重写提示词以冻结快照
	// 为校验与前向修复依据（无快照时返回空串，提示词与旧行为一致）。
	a.pipeline.SetBibleProvider(func(ctx context.Context, novelID int64) (string, error) {
		return a.snapshots.EstablishedFactsText(ctx, novelID)
	})
	// 注入「创作心智」提供者（P0 T2.3 + P1 T3.2）：重写/体检/修复/巡检提示词的 Voice 块正文
	// = 心智正文（技能正文+诚实边界声明，§0.3 provider 口径）+ Voice 台账渲染追加
	// （voice_dimensions 非空时，§0.4 顺序：P0 输出为前缀一字不动）。persona 未配置/
	// 解析失败心智为空；台账为空时返回值与 P0 行为逐字一致（降级纪律）。
	a.pipeline.SetVoiceProvider(a.voiceText)
	// 注入文风漂移检测输入提供者（P2 T2.1）：单章正文（git 文件读取）与 Voice 台账
	// 基线指标（voice.MergeMetrics 装配）。任一缺失/出错/空基线时漂移检测降级零 issue。
	a.pipeline.SetChapterTextReader(func(_ context.Context, novelID, chapterID int64) (string, error) {
		return git.ReadFile(novelID, git.ChapterPath(chapterID))
	})
	a.pipeline.SetVoiceMetricsProvider(func(ctx context.Context, novelID int64) (map[string]float64, error) {
		entries, err := voice.NewStore(a.db, a.logger).List(ctx, novelID)
		if err != nil {
			return nil, err
		}
		return voice.MergeMetrics(entries), nil
	})
	// 注入「创作意图锚定」提供者（P0 T4.1 定源）：book-pitch 应用产物（novels 字段 +
	// 五类 pitch 偏好）→ 当前卷纲正文 → 空串降级。仅 rewrite 用途装配（§0.5 矩阵）。
	a.pipeline.SetIntentProvider(a.intentText)
	// 注入 retcon 审批闸门（WBS 3.1）：S5 交回的写保护/retcon 候选沉淀为持久化候选案，
	// 作者显式审批后才允许受控回改冻结卷（3.2 接快照 diff/Restore 留痕与回滚）。
	a.pipeline.SetRetconGate(a.retcons)
	// R3d GateContext 瘦身：一次性把存量胖 ContextJSON 的 9 大对象表化进
	// gate_objects（幂等；失败只告警，读路径有胖 blob 兜底）。
	a.pipeline.MigrateGateObjects()
	// R3b 持久任务队列：先放弃进程退出残留的活跃任务（跨进程恢复仍走下面的
	// RecoverInterruptedRuns 人工链路，外部恢复语义不变），再启动执行体消费。
	a.pipeline.StartTaskQueue()
	// 启动恢复：把上次进程遗留的「running 僵尸运行」（执行 goroutine 已消亡）统一置为
	// paused 并打中断标记，用户点「恢复」时按阶段重新派发执行体。见 RecoverInterruptedRuns。
	a.pipeline.RecoverInterruptedRuns()

	// 11. 异步初始化向量存储和搜索服务（不阻塞 UI）
	go func() {
		emb, err := rag.GetEmbedder()
		svc := search.NewService(a.logger, a.character, a.location,
			a.timeline, a.storyarc, a.chapter, a.reader, a.preference, a.setting, nil)
		a.searchService.Store(svc)
		a.agent.SetSearchService(svc)
		if err != nil {
			a.logger.Error("获取 Embedder 失败，向量检索不可用", "err", err)
			return
		}
		sqlDB, err := a.db.DB()
		if err != nil {
			a.logger.Error("获取底层 SQL DB 失败，向量检索不可用", "err", err)
			return
		}
		rag.InitVectorStore(sqlDB, emb, a.logger)
		vectorStore := rag.GetVectorStore()
		a.vectorStore.Store(vectorStore)
		a.logger.Info("向量存储初始化完成")

		// 初始化搜索服务
		svc = search.NewService(a.logger, a.character, a.location,
			a.timeline, a.storyarc, a.chapter, a.reader, a.preference, a.setting, vectorStore)
		a.searchService.Store(svc)
		a.agent.SetSearchService(svc)

		// 初始化刷新队列并启动
		rag.InitRefreshQueue(vectorStore, a.chapter, a.novel, a.logger)
		rag.GetRefreshQueue().Start()

		// 注入设定补索引钩子：冷启动/ONNX 后加载时，RebuildAll 会重索引各本小说的设定向量。
		rag.RegisterSettingIndexer(func(ctx context.Context, novelID int64) error {
			return setting.ReindexNovelSettings(ctx, a.db, novelID)
		})

		// 首次启动全量索引（已有向量则跳过）
		rebuildCtx := context.Background()
		if err := rag.GetRefreshQueue().RebuildAll(rebuildCtx); err != nil {
			a.logger.Error("全量向量索引失败", "err", err)
		}
	}()

	// T-AGG-5：应用启动后，自动重建/清理过期的跨书聚合派生档（best-effort，不影响启动）。
	a.reconcileAggregated()

	a.cfg = cfg
	a.logger.Info("应用初始化完成",
		"data_dir", config.DataDirPath(),
		"version", a.GetVersion(),
		"commit_hash", a.GetBuildHash(),
	)
	initSucceeded = true
	return nil
}

// parseModelKey 把 "provider/model" 形式的模型 key 解析为 (provider, model)。
// key 非法或为空时，回退到第一个可用 provider 的第一个模型，保证 S2 蓝图阶段总有可用模型。
func (a *App) parseModelKey(key string) (provider, model string) {
	if key != "" {
		if idx := strings.IndexByte(key, '/'); idx > 0 {
			p, m := key[:idx], key[idx+1:]
			if p != "" && m != "" {
				return p, m
			}
		}
	}
	// 回退：取首个 provider 的首个模型
	for name, p := range a.llmClient.Providers() {
		if len(p.Models) > 0 {
			return name, p.Models[0].ID
		}
	}
	return "", ""
}
