package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"gorm.io/gorm"
)

// RestoreTableSummary 单表回滚结果。
type RestoreTableSummary struct {
	Deleted  int64 `json:"deleted"`
	Inserted int64 `json:"inserted"`
}

// RestoreSummary 整体回滚结果（各表删/插行数），供调用方留痕与提示。
type RestoreSummary struct {
	Tables map[string]RestoreTableSummary `json:"tables"`
}

// restoreTables 把台账表整体恢复到 payload 提交时刻（整体回滚语义）：
//   - 未出现在 payload 的行视为「快照后新增/改动」，随 DELETE 丢弃；
//   - payload 行按原值、原时间戳恢复（created_at/updated_at/changed_at 均不重写）。
//
// 写入走 raw SQL Exec：2.6 temporal/history 回调与 oplog 回调都是全局 gorm
// Create/Update/Delete callback（不检查 SkipHooks），模型写入会戳 changed_at 并
// 追加履历/操作日志；Exec 不进入该回调链，才能保证「回滚不是一次台账编辑」。
//
// 只动 registry 内的台账表；快照行（volume_snapshots）、正文（chapters/Git 文件）
// 与邻接组织表不受影响。
func restoreTables(ctx context.Context, tx *gorm.DB, p *Payload) (*RestoreSummary, error) {
	sum := &RestoreSummary{Tables: make(map[string]RestoreTableSummary, len(registry))}
	for _, spec := range registry {
		res := tx.WithContext(ctx).Exec("DELETE FROM "+spec.name+" WHERE novel_id = ?", p.NovelID)
		if res.Error != nil {
			return nil, fmt.Errorf("snapshot: 回滚清空 %s: %w", spec.name, res.Error)
		}
		ts := RestoreTableSummary{Deleted: res.RowsAffected}

		if raw := p.Tables[spec.name]; len(raw) > 0 {
			n, err := insertRows(ctx, tx, spec, raw)
			if err != nil {
				return nil, err
			}
			ts.Inserted = n
		}
		sum.Tables[spec.name] = ts
	}
	return sum, nil
}

// insertRows 把 payload 行集按原值写回（raw SQL 逐行 INSERT，显式列值含主键与时间戳）。
// 列集合与取值经 gorm schema 反射获得，避免手写列清单漂移。
func insertRows(ctx context.Context, tx *gorm.DB, spec tableSpec, raw json.RawMessage) (int64, error) {
	rowsPtr := spec.newRows()
	if err := json.Unmarshal(raw, rowsPtr); err != nil {
		return 0, fmt.Errorf("snapshot: 解码 %s 行集: %w", spec.name, err)
	}
	rowsVal := reflect.ValueOf(rowsPtr).Elem()
	elemType := rowsVal.Type().Elem()

	stmt := &gorm.Statement{DB: tx}
	if err := stmt.Parse(reflect.New(elemType).Interface()); err != nil {
		return 0, fmt.Errorf("snapshot: 解析 %s 模型: %w", spec.name, err)
	}
	sch := stmt.Schema

	cols := sch.DBNames
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
	query := "INSERT INTO " + sch.Table + " (" + strings.Join(cols, ", ") + ") VALUES (" + placeholders + ")"

	for i := 0; i < rowsVal.Len(); i++ {
		row := rowsVal.Index(i)
		args := make([]any, len(cols))
		for j, name := range cols {
			f := sch.FieldsByDBName[name]
			if f == nil {
				return int64(i), fmt.Errorf("snapshot: %s 缺列 %s", spec.name, name)
			}
			v, _ := f.ValueOf(ctx, row)
			args[j] = v
		}
		if err := tx.WithContext(ctx).Exec(query, args...).Error; err != nil {
			return int64(i), fmt.Errorf("snapshot: 回滚写入 %s: %w", spec.name, err)
		}
	}
	return int64(rowsVal.Len()), nil
}
