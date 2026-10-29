package storage

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"log/slog"
)

// TestOpenConfiguresConnectionPool 验证 B3：放开连接池且 busy_timeout 生效，
// 多连接可并发读写而不报 "database is locked"，且并发自增无丢失更新。
func TestOpenConfiguresConnectionPool(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pool.db")
	db, err := Open(dbPath, slog.Default())
	require.NoError(t, err)
	defer func() { _ = Close(db) }()

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.Greater(t, sqlDB.Stats().MaxOpenConnections, 1,
		"B3: 连接池应放开（至少 2），而非单连接串行")

	// 建表并写入种子
	require.NoError(t, db.Exec("CREATE TABLE kv (k TEXT PRIMARY KEY, v INTEGER)").Error)
	require.NoError(t, db.Exec("INSERT INTO kv (k, v) VALUES ('seed', 0)").Error)

	// 并发：半数写（原子自增）、半数读，触发多连接争用写锁
	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	var mu sync.Mutex
	writeErrs, readErrs := 0, 0
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				if e := db.Exec("UPDATE kv SET v = v + 1 WHERE k = 'seed'").Error; e != nil {
					mu.Lock()
					writeErrs++
					mu.Unlock()
				}
				return
			}
			var v int
			if e := db.Raw("SELECT v FROM kv WHERE k = 'seed'").Scan(&v).Error; e != nil {
				mu.Lock()
				readErrs++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	require.Equal(t, 0, writeErrs, "并发写不应报 database is locked")
	require.Equal(t, 0, readErrs, "并发读不应失败")

	// 最终值必须恰好为写次数（无丢失更新）
	var final int
	require.NoError(t, db.Raw("SELECT v FROM kv WHERE k = 'seed'").Scan(&final).Error)
	require.Equal(t, n/2, final, "并发自增结果应无丢失更新")
}
