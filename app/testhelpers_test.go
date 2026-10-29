package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/activity"
	"github.com/novelgo/novelgo/internal/agent"
	"github.com/novelgo/novelgo/internal/approval"
	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/content"
	"github.com/novelgo/novelgo/internal/deletion"
	"github.com/novelgo/novelgo/internal/event"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/migrate"
	"github.com/novelgo/novelgo/internal/novel"
	"github.com/novelgo/novelgo/internal/preference"
	"github.com/novelgo/novelgo/internal/reader"
	"github.com/novelgo/novelgo/internal/retcon"
	"github.com/novelgo/novelgo/internal/rollback"
	"github.com/novelgo/novelgo/internal/session"
	"github.com/novelgo/novelgo/internal/setting"
	"github.com/novelgo/novelgo/internal/skill"
	"github.com/novelgo/novelgo/internal/snapshot"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/storyarc"
	"github.com/novelgo/novelgo/internal/style"
	"github.com/novelgo/novelgo/internal/testsupport"
	"github.com/novelgo/novelgo/internal/timeline"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/novelgo/novelgo/internal/writing"
)

// setupTestApp creates a fully initialized App with a temp directory,
// in-memory SQLite, and all stores. Resources are cleaned up via t.Cleanup.
func setupTestApp(t *testing.T) *App {
	t.Helper()

	tmpDir := testsupport.Isolate(t)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Set global config so config.DataDirPath() etc. resolve to tmpDir.
	cfg := &config.AppConfig{DataDir: tmpDir}
	config.Set(cfg)

	// Ensure novel sub-directories exist.
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "novels"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "skills"), 0o755))

	// In-memory SQLite (no CGO needed).
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "open in-memory SQLite")

	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})

	// Run auto-migration (pure GORM, no CGO).
	require.NoError(t, migrate.Run(db, logger), "run migrations")

	activityStore := activity.NewStore(db, logger)
	activityStore.InitTracking(context.Background())

	// Load settings (creates default row).
	settings, err := config.LoadSettings(db)
	require.NoError(t, err, "load settings")

	// Register operation log hooks.
	require.NoError(t, storage.RegisterOplogHooks(db))

	// Domain stores.
	novelStore := novel.NewStore(db, logger)
	preferenceStore := preference.NewStore(db, logger)
	settingStore := setting.NewStore(db, logger)
	chapterStore := chapter.NewStore(db, logger)
	characterStore := character.NewStore(db, logger)
	sessionStore := session.NewStore(db, logger)
	timelineStore := timeline.NewStore(db, logger)
	storyarcStore := storyarc.NewStore(db, logger)
	locationStore := location.NewStore(db, logger)
	readerStore := reader.NewStore(db, logger)
	turnCommitStore := rollback.NewStore(db, logger)
	writingStore := writing.NewStore(db, logger)
	styleStore := style.NewStore(db, logger)
	volumeStore := volume.NewStore(db, logger)

	skillStore, err := skill.NewStore(logger, filepath.Join(tmpDir, "skills"))
	require.NoError(t, err, "init skill store")

	// MCP tool registry.
	registry := mcp_tools.NewRegistry(logger)
	mcp_tools.RegisterAllTools(registry)

	// LLM client with empty providers (no real API calls in tests).
	providers := llm.Merge(llm.Builtin, &llm.UserLLMConfig{})
	llmClient := llm.NewClient(providers, logger)

	// Approval service.
	approvals := approval.NewService(logger, settings.ApprovalMode)

	// Cancel manager.
	cancelMgr := agent.NewCancelManager()

	// Agent.
	ag := agent.New(llmClient, registry, sessionStore, chapterStore, db, approvals, logger, skillStore, cancelMgr, event.NopEmitter{})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	app := &App{
		ctx:       ctx,
		cancel:    cancel,
		logger:    logger,
		cfg:       cfg,
		settings:  settings,
		db:        db,
		llmClient: llmClient,
		agent:     ag,
		cancelMgr: cancelMgr,
		registry:  registry,
		approvals: approvals,

		novel:      novelStore,
		preference: preferenceStore,
		setting:    settingStore,
		chapter:    chapterStore,
		character:  characterStore,
		session:    sessionStore,
		skill:      skillStore,
		style:      styleStore,
		timeline:   timelineStore,
		storyarc:   storyarcStore,
		location:   locationStore,
		reader:     readerStore,
		turnCommit: turnCommitStore,
		writing:    writingStore,
		volume:     volumeStore,
		snapshots:  snapshot.NewService(db, volumeStore, chapterStore),
		activity:   activityStore,
	}
	// retcon 审批闸门（WBS 3.1/3.2）：与 handler.go 装配同形（三执行体接 snapshot 服务）。
	app.retcons = retcon.NewService(retcon.NewStore(db))
	app.retcons.SetCommitSnapshot(func(ctx context.Context, novelID, volumeID int64) (int64, error) {
		snap, err := app.snapshots.Commit(ctx, novelID, volumeID)
		if err != nil {
			return 0, err
		}
		return snap.ID, nil
	})
	app.retcons.SetDiffSummary(func(ctx context.Context, snapshotID int64) (string, error) {
		delta, err := app.snapshots.DiffAgainstCurrent(ctx, snapshotID)
		if err != nil {
			return "", err
		}
		return snapshot.FormatDelta(delta), nil
	})
	app.retcons.SetRestorer(func(ctx context.Context, _, snapshotID int64) error {
		_, err := app.snapshots.Restore(ctx, snapshotID)
		return err
	})
	// 风格案（kind=style）回滚只还原 voice_dimensions——与 handler.go 装配同形。
	app.retcons.SetStyleRestorer(func(ctx context.Context, _, snapshotID int64) error {
		_, err := app.snapshots.RestoreStyle(ctx, snapshotID)
		return err
	})
	chapterDeletionGuard := deletion.NewGuard(
		deletion.For(deletion.EntityChapter, timelineStore.ChapterDeletionBlockers),
		deletion.For(deletion.EntityChapter, storyarcStore.ChapterDeletionBlockers),
		deletion.For(deletion.EntityChapter, readerStore.ChapterDeletionBlockers),
		deletion.For(deletion.EntityChapter, characterStore.ChapterDeletionBlockers),
	)
	app.chapterService = chapter.NewService(chapterStore, volumeStore, chapterDeletionGuard, logger, nil, nil)
	// 保存正文后的跨介质派生编排服务（搜索缓存懒加载器测试环境传 nil）。
	app.contentService = content.NewService(logger, chapterStore, writingStore, nil)

	return app
}

// createTestNovel creates a novel in the database and returns it.
// It also ensures the novel's directory exists on disk.
func createTestNovel(t *testing.T, app *App) *novel.Novel {
	t.Helper()

	n := &novel.Novel{
		Title:       "Test Novel",
		Genre:       "fantasy",
		Description: "A novel for testing",
	}
	require.NoError(t, app.db.Create(n).Error, "create test novel")

	// Ensure the novel's directory exists (normally done by the git init flow).
	novelDir := config.NovelDirPath(n.ID)
	require.NoError(t, os.MkdirAll(novelDir, 0o755), "create novel dir")

	return n
}

func int64Ptr(v int64) *int64 {
	return &v
}

func createTestChapter(t *testing.T, app *App, novelID int64) *chapter.Chapter {
	t.Helper()
	ch, err := app.CreateChapter(CreateChapterInput{NovelID: novelID, Title: "Test Chapter"})
	require.NoError(t, err, "create test chapter")
	return ch
}
