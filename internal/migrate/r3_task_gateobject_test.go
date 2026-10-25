//go:build cgo

package migrate_test

// r3_task_gateobject_test.go 是 R3 任务队列化的迁移集成测试
// （pipeline-core-redesign-proposal.md §4.4：R3 中高风险=触及恢复路径，需迁移
// 集成测试）。从 R3 前的流水线库（胖 ContextJSON + 残留 interrupted/dispatching
// 键、无 tasks/gate_objects 表）出发，只调用公开入口 migrate.Run，验收：
//  ① tasks/gate_objects 新表落位且可用（R3a 队列、R3d 大对象表化）；
//  ② 版本化迁移状态不涨（R3 只 AutoMigrate 加表，done 仍 12）；
//  ③ 旧胖 ContextJSON 读路径可见（恢复链路 GetByRunID/ListByStatus 的输入语义
//     不变，R3c 删除的补丁布尔键残留无害）；
//  ④ MigrateGateObjects 一次性表化（blob 瘦身+行落地、视图不变、幂等）；
//  ⑤ 迁移后的写口拆合往返不丢大对象（恢复后的续跑链路所依赖）。

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/novelgo/novelgo/internal/migrate"
	"github.com/novelgo/novelgo/internal/pipeline"
	"github.com/novelgo/novelgo/internal/task"
)

func TestRunUpgradesPreR3PipelineDatabaseForTaskQueueAndGateObjects(t *testing.T) {
	setupMigrationIntegrationEnv(t)
	db := openMigrationIntegrationDB(t)
	execSQL := func(query string) {
		t.Helper()
		if err := db.Exec(query).Error; err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}

	// R3 前的 pipeline_runs（列形与当前一致，R3 只动 ContextJSON 内容语义）。
	execSQL(`CREATE TABLE pipeline_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		novel_id INTEGER NOT NULL,
		run_id TEXT NOT NULL,
		mode TEXT NOT NULL DEFAULT 'new',
		start_chapter INTEGER,
		current_stage TEXT NOT NULL,
		status TEXT NOT NULL,
		auto_run BOOLEAN NOT NULL DEFAULT 0,
		context_json TEXT,
		volume_id INTEGER,
		parent_run_id TEXT,
		created_at DATETIME,
		updated_at DATETIME
	)`)
	execSQL(`CREATE UNIQUE INDEX idx_pipeline_runs_run_id ON pipeline_runs(run_id)`)

	// 胖 ContextJSON：9 大对象 + 决策小字段 + R3c 已退役补丁布尔键残留。
	fatA := `{"stages":["S4"],"gates":[{"gate_id":"G1","decision":"continue","at":"2026-10-01T00:00:00Z"}],` +
		`"blueprint":"{\"volumes\":[{\"id\":1}]}","audit":"{\"verdict\":\"needs_fix\",\"score\":28,\"summary\":\"体检结论\"}",` +
		`"blueprint_audit":"{\"verdict\":\"pass\"}","drift_report":"第3章与卷纲冲突","stock_audit":"{\"verdict\":\"pass\"}",` +
		`"patrol_report":"{\"verdict\":\"needs_fix\"}","rewrite_backlog":[{"id":"I2","level":"P1","chapters":[4,5]}],` +
		`"retcon_backlog":[{"id":"I3","target_scope":"frozen_volume"}],"manual_backlog":[{"id":"I4","reason":"skipped"}],` +
		`"s4_audited":true,"audit_stage":"S4","audit_reason":"milestone","fix_states":{"I1":"fixed"},` +
		`"interrupted":true,"dispatching":false}`
	fatB := `{"stages":["S5"],"audit":"{\"verdict\":\"pass\",\"score\":36}","s4_audited":true,"interrupted":false}`
	execSQL(`INSERT INTO pipeline_runs (id, novel_id, run_id, mode, current_stage, status, context_json) VALUES ` +
		`(1, 1, 'legacy-a', 'new', 'S4', 'running', '` + strings.ReplaceAll(fatA, "'", "''") + `')`)
	execSQL(`INSERT INTO pipeline_runs (id, novel_id, run_id, mode, current_stage, status, context_json) VALUES ` +
		`(2, 1, 'legacy-b', 'new', 'S5', 'waiting', '` + strings.ReplaceAll(fatB, "'", "''") + `')`)

	if err := migrate.Run(db, slog.Default()); err != nil {
		t.Fatalf("migrate.Run: %v", err)
	}

	// ① R3 新表落位。
	for _, table := range []string{"tasks", "gate_objects", "pipeline_runs"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("R3 新表缺失: %s", table)
		}
	}
	// ② R3 不加版本化迁移：done 总数仍是 12。
	var done int64
	if err := db.Table("migrate_state").Where("status = ?", "done").Count(&done).Error; err != nil {
		t.Fatal(err)
	}
	if done != 12 {
		t.Fatalf("版本化迁移 done 步数 = %d, want 12（R3 只 AutoMigrate 加表）", done)
	}

	ctx := context.Background()
	store := pipeline.NewStore(db)

	// ③ 旧胖 ContextJSON 读路径可见（恢复链路输入语义不变；残留
	// interrupted/dispatching 键由 json.Unmarshal 忽略，无兼容问题）。
	runA, err := store.GetByRunID(ctx, "legacy-a")
	if err != nil {
		t.Fatalf("GetByRunID legacy-a: %v", err)
	}
	assertLegacyFatContext := func(t *testing.T, raw string) {
		t.Helper()
		var ctxt pipeline.GateContext
		if err := json.Unmarshal([]byte(raw), &ctxt); err != nil {
			t.Fatalf("旧上下文解包失败: %v", err)
		}
		if !strings.Contains(ctxt.Audit, "needs_fix") {
			t.Fatalf("audit 丢失: %q", ctxt.Audit)
		}
		if len(ctxt.RewriteBacklog) != 1 || ctxt.RewriteBacklog[0].ID != "I2" {
			t.Fatalf("rewrite_backlog 丢失: %+v", ctxt.RewriteBacklog)
		}
		if len(ctxt.RetconBacklog) != 1 || len(ctxt.ManualBacklog) != 1 {
			t.Fatalf("回流清单丢失: %+v / %+v", ctxt.RetconBacklog, ctxt.ManualBacklog)
		}
		for _, s := range []string{ctxt.Blueprint, ctxt.BlueprintAudit, ctxt.StockAudit, ctxt.PatrolReport, ctxt.DriftReport} {
			if s == "" {
				t.Fatal("大对象字段丢失")
			}
		}
	}
	assertLegacyFatContext(t, runA.ContextJSON)
	// 恢复链路的扫描入口（RecoverInterruptedRuns 扫 running）也拿到胖视图。
	running, err := store.ListByStatus(ctx, pipeline.StatusRunning)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(running) != 1 || running[0].RunID != "legacy-a" {
		t.Fatalf("恢复扫描应看到 legacy-a: %+v", running)
	}
	assertLegacyFatContext(t, running[0].ContextJSON)

	// ④ MigrateGateObjects：胖 blob→表行+瘦 blob，视图不变，幂等。
	n, err := store.MigrateGateObjects(ctx)
	if err != nil {
		t.Fatalf("MigrateGateObjects: %v", err)
	}
	if n != 2 {
		t.Fatalf("表化 run 数 = %d, want 2", n)
	}
	var thinA string
	if err := db.Raw(`SELECT context_json FROM pipeline_runs WHERE run_id = 'legacy-a'`).Scan(&thinA).Error; err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"audit", "rewrite_backlog", "manual_backlog", "patrol_report"} {
		if strings.Contains(thinA, `"`+key+`"`) {
			t.Fatalf("迁移后瘦 blob 残留大对象键 %s: %s", key, thinA)
		}
	}
	var objs int64
	if err := db.Table("gate_objects").Where("run_id = ?", "legacy-a").Count(&objs).Error; err != nil {
		t.Fatal(err)
	}
	if objs != 9 {
		t.Fatalf("legacy-a 大对象行数 = %d, want 9", objs)
	}
	runA2, err := store.GetByRunID(ctx, "legacy-a")
	if err != nil {
		t.Fatalf("迁移后 GetByRunID: %v", err)
	}
	assertLegacyFatContext(t, runA2.ContextJSON)
	if n, err := store.MigrateGateObjects(ctx); err != nil || n != 0 {
		t.Fatalf("迁移应幂等: n=%d err=%v", n, err)
	}

	// ⑤ 迁移后的写口拆合往返不丢大对象（恢复续跑链路依赖）。
	if err := store.SetStatus(ctx, "legacy-a", pipeline.StatusPaused, runA2.ContextJSON); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	runA3, err := store.GetByRunID(ctx, "legacy-a")
	if err != nil {
		t.Fatalf("写后 GetByRunID: %v", err)
	}
	if runA3.Status != pipeline.StatusPaused {
		t.Fatalf("status 未更新: %s", runA3.Status)
	}
	assertLegacyFatContext(t, runA3.ContextJSON)
	var thinAfter string
	if err := db.Raw(`SELECT context_json FROM pipeline_runs WHERE run_id = 'legacy-a'`).Scan(&thinAfter).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(thinAfter, `"audit"`) {
		t.Fatalf("写口应继续拆合（瘦 blob）: %s", thinAfter)
	}

	// 附带验收：任务队列表经 migrate.Run 落位后可正常入队（R3a）。
	ts := task.NewStore(db, time.Now)
	if _, _, err := ts.Enqueue(ctx, task.Task{
		Kind: "audit", DedupKey: "audit:legacy-a", RunID: "legacy-a", NovelID: 1,
	}); err != nil {
		t.Fatalf("tasks 表入队失败: %v", err)
	}
	var tcount int64
	if err := db.Table("tasks").Count(&tcount).Error; err != nil {
		t.Fatal(err)
	}
	if tcount != 1 {
		t.Fatalf("tasks 行数 = %d, want 1", tcount)
	}
}
