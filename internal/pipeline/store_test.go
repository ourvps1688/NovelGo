package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	// 内存库 + 连接池会偶发「写入与查询落在不同连接」导致数据不可见，
	// 强制单连接保证同测试内数据可见（真实文件库无此问题）。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&PipelineRun{}, &PipelineIssue{}); err != nil {
		t.Fatalf("migrate pipeline_runs: %v", err)
	}
	return db
}

func TestStoreCreateGetByNovel(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	run := &PipelineRun{NovelID: 7, RunID: "r1", Mode: "new", CurrentStage: StageS1, Status: StatusRunning}
	if err := s.Create(ctx, run); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetByNovel(ctx, 7)
	if err != nil {
		t.Fatalf("getByNovel: %v", err)
	}
	if got.RunID != "r1" || got.CurrentStage != StageS1 {
		t.Fatalf("unexpected run: %+v", got)
	}
	// 已结束的 run 不应被 GetByNovel 返回
	_ = s.SetStatus(ctx, "r1", StatusFinished, "")
	if _, err := s.GetByNovel(ctx, 7); err == nil {
		t.Fatalf("已完成的流水线不应被 GetByNovel 返回")
	}
}

func TestStoreStatusFlow(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	run := &PipelineRun{NovelID: 8, RunID: "r2", CurrentStage: StageS1, Status: StatusRunning}
	if err := s.Create(ctx, run); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.SetStatus(ctx, "r2", StatusPaused, ""); err != nil {
		t.Fatalf("setstatus: %v", err)
	}
	got, _ := s.GetByRunID(ctx, "r2")
	if got.Status != StatusPaused {
		t.Fatalf("status not paused: %s", got.Status)
	}
	// 阶段推进
	if err := s.SetStageStatus(ctx, "r2", StageS3, StatusRunning, ""); err != nil {
		t.Fatalf("setstagestatus: %v", err)
	}
	got, _ = s.GetByRunID(ctx, "r2")
	if got.CurrentStage != StageS3 || got.Status != StatusRunning {
		t.Fatalf("stage status unexpected: %+v", got)
	}
}

func TestStoreContextJSONRoundTrip(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	run := &PipelineRun{NovelID: 9, RunID: "r3", CurrentStage: StageS1, Status: StatusRunning, ContextJSON: `{"gates":[]}`}
	if err := s.Create(ctx, run); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AppendGateDecision(ctx, "r3", "G1", "continue", "ok"); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, _ := s.GetByRunID(ctx, "r3")
	var ctxt GateContext
	if err := json.Unmarshal([]byte(got.ContextJSON), &ctxt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ctxt.Gates) != 1 || ctxt.Gates[0].GateID != "G1" {
		t.Fatalf("unexpected gates: %+v", ctxt.Gates)
	}
	// 再次追加，应累积
	if err := s.AppendGateDecision(ctx, "r3", "G1", "modify", "tweak"); err != nil {
		t.Fatalf("append2: %v", err)
	}
	got, _ = s.GetByRunID(ctx, "r3")
	_ = json.Unmarshal([]byte(got.ContextJSON), &ctxt)
	if len(ctxt.Gates) != 2 {
		t.Fatalf("gates not accumulated: %d", len(ctxt.Gates))
	}
}
