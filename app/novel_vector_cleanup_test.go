package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/rag"
)

// 删除小说必须连带 DROP 它的向量表，否则派生索引会永久残留。
// app 层只验证 DeleteNovel 调用了 VectorStore.DeleteNovel；vec0 模块和影子表
// 的行为由 rag 包的真实 vec0 用例覆盖，避免 sqlite-vec 全局注册导致测试顺序耦合。
func TestDeleteNovel_DropsVectorTable(t *testing.T) {
	a := setupTestApp(t)
	n := createTestNovel(t, a)

	sqlDB, err := a.db.DB()
	require.NoError(t, err)

	// 用同名普通表验证 app 层删除路径。SQLite 的 DROP TABLE 对虚拟表和普通表
	// 使用同一语义，真实 vec0 行为由 rag 包单测覆盖。
	tableName := fmt.Sprintf("vec_novel_%d", n.ID)
	_, err = sqlDB.ExecContext(context.Background(), fmt.Sprintf("CREATE TABLE %s (id INTEGER)", tableName))
	require.NoError(t, err, "create vector table")

	// DeleteNovel 只用 VectorStore 的 db，不碰 embedder（测试环境无 ONNX），传 nil 即可。
	a.vectorStore.Store(rag.NewVectorStore(sqlDB, nil, a.logger))

	tableExists := func() bool {
		var n int
		require.NoError(t, a.db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", tableName).Scan(&n).Error)
		return n == 1
	}
	require.True(t, tableExists(), "向量表应在删除前存在")

	require.NoError(t, a.DeleteNovel(n.ID))

	assert.False(t, tableExists(), "向量表应随小说删除一起 DROP")
}
