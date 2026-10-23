package retcon

// retcon 风格回改（P2 T4.2）：Kind 归一/校验 + Rollback 按 kind 路由执行体。
// A3 状态机零改动：本文件不触碰迁移表；存量 fact 案（含空 kind 存量形态）走
// 全量 Restorer 的行为由既有测试逐字钉死。

import (
	"context"
	"errors"
	"testing"
)

// TestCreateCandidateKindDefaultAndValidation 验证 Kind 归一：缺省 fact（存量
// 行为逐字不变）、显式 style 保留、枚举外值拒绝（宁缺勿错）。
func TestCreateCandidateKindDefaultAndValidation(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(openRetconDB(t))

	c, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, Title: "吃书申请"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Kind != KindFact {
		t.Errorf("缺省 kind = %q, want %q", c.Kind, KindFact)
	}

	s, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, Kind: KindStyle, Title: "文风回改申请"})
	if err != nil {
		t.Fatalf("create style: %v", err)
	}
	if s.Kind != KindStyle {
		t.Errorf("style kind = %q, want %q", s.Kind, KindStyle)
	}

	if _, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, Kind: "plot", Title: "非法 kind"}); err == nil {
		t.Error("枚举外 kind 应拒绝")
	}
}

// toRecorded 推进一条案到 recorded 态（供回滚路由测试复用）。
func toRecorded(t *testing.T, svc *Service, in CreateInput) *RetconCase {
	t.Helper()
	ctx := context.Background()
	c, err := svc.CreateCandidate(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Approve(ctx, c.ID, "author", "批准"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := svc.BeginEdit(ctx, c.ID, "author", 7); err != nil {
		t.Fatalf("begin edit: %v", err)
	}
	if _, err := svc.Record(ctx, c.ID, "author", "diff"); err != nil {
		t.Fatalf("record: %v", err)
	}
	return c
}

// TestRollbackRoutesByKind 验证 Rollback 按 kind 路由执行体：style 案只走
// StyleRestorer（全量 Restorer 不被触碰）、fact 案只走全量 Restorer、
// style 案未接 StyleRestorer 拒绝执行且不得误调全量 Restorer。
func TestRollbackRoutesByKind(t *testing.T) {
	ctx := context.Background()

	// style 案：走 styleRestorer，全量 restorer 零触碰。
	t.Run("style 走风格执行体", func(t *testing.T) {
		svc := newTestService(openRetconDB(t))
		c := toRecorded(t, svc, CreateInput{NovelID: 1, Kind: KindStyle, Title: "文风回改"})
		factCalled, styleCalled := false, false
		svc.SetRestorer(func(context.Context, int64, int64) error { factCalled = true; return nil })
		svc.SetStyleRestorer(func(context.Context, int64, int64) error { styleCalled = true; return nil })
		rolled, err := svc.Rollback(ctx, c.ID, "author", "撤销")
		if err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if !styleCalled {
			t.Error("style 案应走 StyleRestorer")
		}
		if factCalled {
			t.Error("style 案不得触碰全量 Restorer")
		}
		if rolled.Status != StatusRolledBack {
			t.Errorf("status = %q, want rolled_back", rolled.Status)
		}
	})

	// fact 案（显式）：走全量 restorer，styleRestorer 零触碰。
	t.Run("fact 走全量执行体", func(t *testing.T) {
		svc := newTestService(openRetconDB(t))
		c := toRecorded(t, svc, CreateInput{NovelID: 1, Kind: KindFact, Title: "事实回改"})
		factCalled, styleCalled := false, false
		svc.SetRestorer(func(context.Context, int64, int64) error { factCalled = true; return nil })
		svc.SetStyleRestorer(func(context.Context, int64, int64) error { styleCalled = true; return nil })
		if _, err := svc.Rollback(ctx, c.ID, "author", "撤销"); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if !factCalled {
			t.Error("fact 案应走全量 Restorer")
		}
		if styleCalled {
			t.Error("fact 案不得触碰 StyleRestorer")
		}
	})

	// 存量形态：kind 为空的旧案仍走全量 restorer（行为逐字不变）。
	t.Run("存量空 kind 走全量执行体", func(t *testing.T) {
		svc := newTestService(openRetconDB(t))
		c := toRecorded(t, svc, CreateInput{NovelID: 1, Title: "存量案"})
		if err := svc.store.db.Model(&RetconCase{}).Where("id = ?", c.ID).
			Update("kind", "").Error; err != nil {
			t.Fatalf("清空 kind 模拟存量行: %v", err)
		}
		factCalled := false
		svc.SetRestorer(func(context.Context, int64, int64) error { factCalled = true; return nil })
		if _, err := svc.Rollback(ctx, c.ID, "author", "撤销"); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if !factCalled {
			t.Error("存量空 kind 案应走全量 Restorer")
		}
	})

	// style 案未接 StyleRestorer：拒绝执行（绝不名义回滚），且不得误调全量 Restorer。
	t.Run("style 缺风格执行体拒绝回滚", func(t *testing.T) {
		svc := newTestService(openRetconDB(t))
		c := toRecorded(t, svc, CreateInput{NovelID: 1, Kind: KindStyle, Title: "文风回改"})
		factCalled := false
		svc.SetRestorer(func(context.Context, int64, int64) error { factCalled = true; return nil })
		if _, err := svc.Rollback(ctx, c.ID, "author", "撤销"); !errors.Is(err, ErrNoRestorer) {
			t.Errorf("缺风格执行体回滚 = %v, want ErrNoRestorer", err)
		}
		if factCalled {
			t.Error("style 案缺执行体时不得降级误调全量 Restorer")
		}
		got, err := svc.store.GetByID(ctx, c.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Status != StatusRecorded {
			t.Errorf("拒绝回滚后状态 = %q, want recorded", got.Status)
		}
	})
}
