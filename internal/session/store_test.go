package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// newMemoryStore 打开内存 SQLite（单连接，避免 :memory: 每连接独立库），
// 自动迁移 Session/Message 并返回可用的 Store。
func newMemoryStore(t *testing.T) *Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1) // 内存库单连接，保证查到的数据一致
	require.NoError(t, db.AutoMigrate(&Session{}, &Message{}))
	return NewStore(db, discardLogger())
}

// TestStore_FrontendMessagesOrderedByIdTiebreaker 验证同秒写入的消息按 id 升序稳定返回，
// 不再依赖非确定的 created_at 排序（修复维度 B1：消息排序无 id tiebreaker）。
func TestStore_FrontendMessagesOrderedByIdTiebreaker(t *testing.T) {
	s := newMemoryStore(t)
	ctx := context.Background()
	sameTime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	for i := 0; i < 6; i++ {
		m := &Message{
			SessionID:  "sess_tie",
			Role:       "user",
			Content:    fmt.Sprintf("m-%d", i),
			CreatedAt:  sameTime, // 强制同秒，逼出 id tiebreaker 的作用
			ToFrontend: true,
		}
		require.NoError(t, s.DB.Create(m).Error)
	}

	got, err := s.GetMessagesForFrontend(ctx, "sess_tie")
	require.NoError(t, err)
	require.Len(t, got, 6)

	ids := make([]int64, len(got))
	for i := 1; i < len(got); i++ {
		ids[i-1] = got[i-1].ID
		if got[i-1].ID > got[i].ID {
			ids[i] = got[i].ID
			t.Fatalf("GetMessagesForFrontend 顺序不确定，未应用 id tiebreaker: ids=%v", ids)
		}
		// 断言 created_at 确实相同，确保本测试真正触发了 tiebreaker 分支。
		if !got[i-1].CreatedAt.Equal(got[i].CreatedAt) {
			t.Fatalf("测试假设被破坏：created_at 已不同，tiebreaker 未被触发")
		}
	}
	ids[len(got)-1] = got[len(got)-1].ID
}

// TestStore_APIMessagesOrderedByIdTiebreaker 验证 GetMessagesForAPI 同样按 id 稳定排序，
// 保证回放给 LLM 的上下文顺序确定（同一时刻批量注入的消息不再乱序）。
func TestStore_APIMessagesOrderedByIdTiebreaker(t *testing.T) {
	s := newMemoryStore(t)
	ctx := context.Background()
	sameTime := time.Date(2020, 5, 6, 7, 8, 9, 0, time.UTC)

	for i := 0; i < 4; i++ {
		m := &Message{
			SessionID:  "sess_api",
			Role:       "user",
			Content:    fmt.Sprintf("a-%d", i),
			CreatedAt:  sameTime,
			ToAPI:      true,
			ToFrontend: false,
		}
		require.NoError(t, s.DB.Create(m).Error)
	}

	got, err := s.GetMessagesForAPI(ctx, "sess_api", 1)
	require.NoError(t, err)
	require.Len(t, got, 4)
	for i := 1; i < len(got); i++ {
		if got[i-1].ID > got[i].ID {
			t.Fatalf("GetMessagesForAPI 未按 id 升序排序: 第 %d 条 id=%d > 第 %d 条 id=%d",
				i-1, got[i-1].ID, i, got[i].ID)
		}
	}
}
