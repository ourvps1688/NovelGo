package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// SQLite 连接池配置（B3：放开读写并发）。
//
// SQLite 在 WAL 模式下支持「一写多读」并发：写事务获取写锁串行执行，
// 读连接可同时在 -wal 上做快照读。原代码 SetMaxOpenConns(1) 把所有读写
// 强制串行，成为规模化性能瓶颈（chat 流式读写、检测任务、UI 查询相互阻塞）。
//
// 放开连接池后必须设置 busy_timeout：并发写者不再立即报 "database is locked"，
// 而是自旋等待写锁释放，从而保证并发安全。该参数经 DSN 注入——mattn 驱动会在
// 每个新建的物理连接上自动应用（连接池会创建多个连接，单次 PRAGMA Exec 无法覆盖后续新建连接）。
const (
	// sqliteBusyTimeout 写锁等待上限。超过即报 locked，避免无限阻塞。
	sqliteBusyTimeout = 5 * time.Second
	// defaultMaxIdleConns 空闲连接上限，避免连接数无意义膨胀。
	defaultMaxIdleConns = 4
)

// Open 打开 SQLite 数据库，启用 WAL 模式、sqlite-vec 向量扩展与并发连接池。
//
//	dsn: SQLite 文件路径，如 "/data/my-novel/novel.db"
func Open(dsn string, log *slog.Logger) (*gorm.DB, error) {
	// 启用 sqlite-vec 扩展（全局注册，所有 mattn/go-sqlite3 连接自动可用）
	sqlite_vec.Auto()

	// busy_timeout 必须按连接设置：经 DSN 参数注入，mattn 在每次建连时自动应用，
	// 覆盖整个连接池（含后续新建连接）。journal_mode=WAL 同理由 DSN 兜底，
	// 与下方 enableWAL 验证形成双重保险。
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	pooledDSN := fmt.Sprintf("%s%s_busy_timeout=%d&_journal_mode=WAL",
		dsn, sep, sqliteBusyTimeout.Milliseconds())

	db, err := gorm.Open(sqlite.Open(pooledDSN), &gorm.Config{
		Logger: &gormSlogAdapter{log: log},
	})
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}

	// 配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("storage: 获取 sql.DB 失败: %w", err)
	}

	// 写连接在 SQLite 层仍串行（单写锁），但读连接可并发，整体吞吐随并发
	// 读写比例提升。上限取 CPU 数（桌面单进程场景足够），下限 4 避免小核机器
	// 退化回接近串行，上限 8 避免过多写者无谓争用同一把写锁。
	maxOpen := runtime.NumCPU()
	switch {
	case maxOpen < 4:
		maxOpen = 4
	case maxOpen > 8:
		maxOpen = 8
	}
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(defaultMaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Hour) // 连接最长存活 1 小时

	if err := enableWAL(sqlDB); err != nil {
		return nil, fmt.Errorf("storage: 启用 WAL 失败: %w", err)
	}

	log.Info("SQLite 已打开", "dsn", dsn, "max_open_conns", maxOpen,
		"busy_timeout_ms", sqliteBusyTimeout.Milliseconds())
	return db, nil
}

// enableWAL 开启 WAL 模式，支持一写多读并发。
func enableWAL(db *sql.DB) error {
	var journal string
	if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&journal); err != nil {
		return err
	}
	if journal != "wal" {
		return fmt.Errorf("期望 journal_mode=wal，实际为 %s", journal)
	}
	return nil
}

// Close 关闭数据库并清理 sqlite-vec 全局状态。
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("storage: 获取 sql.DB 失败: %w", err)
	}

	sqlite_vec.Cancel()

	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("storage: 关闭数据库失败: %w", err)
	}
	return nil
}

// Ping 检查数据库连接是否正常。
func Ping(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("storage: 获取 sql.DB 失败: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("storage: ping 失败: %w", err)
	}
	return nil
}

// gormSlogAdapter 将 slog 适配为 GORM 的 logger 接口。
type gormSlogAdapter struct {
	log *slog.Logger
}

func (a *gormSlogAdapter) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	// 日志级别由 slog 的 HandlerOptions.Level 控制，此处不做二次过滤
	return a
}

func (a *gormSlogAdapter) Info(_ context.Context, msg string, args ...any) {
	a.log.Info(msg, args...)
}

func (a *gormSlogAdapter) Warn(_ context.Context, msg string, args ...any) {
	a.log.Warn(msg, args...)
}

func (a *gormSlogAdapter) Error(_ context.Context, msg string, args ...any) {
	a.log.Error(msg, args...)
}

func (a *gormSlogAdapter) Trace(_ context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	attrs := []any{
		"耗时", elapsed,
		"行数", rows,
		"sql", sql,
	}
	if err != nil {
		attrs = append(attrs, "错误", err)
		// 未命中记录是正常业务分支（空查询/可选查询）而非异常：降为 Debug，
		// 避免队列心跳等高频空查询把「GORM 查询异常」刷满应用日志（排障噪声源）。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			a.log.Debug("GORM 查询未命中", attrs...)
			return
		}
		a.log.Warn("GORM 查询异常", attrs...)
		return
	}
	a.log.Debug("GORM 查询", attrs...)
}
