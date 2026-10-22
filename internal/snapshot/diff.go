package snapshot

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
)

// FieldDelta 单字段变化（Old=基准侧，New=目标侧；缺失侧为 null）。
type FieldDelta struct {
	Old any `json:"old"`
	New any `json:"new"`
}

// RowDelta 行级变化：按主键 id 匹配后的字段差异。
type RowDelta struct {
	ID     int64                 `json:"id"`
	Fields map[string]FieldDelta `json:"fields"`
}

// TableDelta 表级差异。Added/Removed 为整行（留痕可读），Changed 只列变化字段。
type TableDelta struct {
	Table   string           `json:"table"`
	Added   []map[string]any `json:"added,omitempty"`
	Removed []map[string]any `json:"removed,omitempty"`
	Changed []RowDelta       `json:"changed,omitempty"`
}

// Delta 两份快照 payload 的结构化差异（WBS 2.2「支持 diff」）。
// Tables 仅含有差异的表，顺序=registry 顺序；无差异时为空。
type Delta struct {
	SchemaVersion int          `json:"schema_version"`
	BaseVersion   int          `json:"base_version"`
	TargetVersion int          `json:"target_version"`
	Tables        []TableDelta `json:"tables"`
}

// Diff 比较两份快照 payload（base 为 nil 时视为空基线=目标全量新增）。
// 行按主键 id 匹配；行内容比较用 json.Number 保数值保真（避免 float64 精度丢失）。
func Diff(base, target *Payload) *Delta {
	d := &Delta{SchemaVersion: SchemaVersion, Tables: []TableDelta{}}
	if target == nil {
		return d
	}
	d.TargetVersion = target.SnapshotVersion
	if base != nil {
		d.BaseVersion = base.SnapshotVersion
	}

	for _, spec := range registry {
		baseRows := decodeRows(rawTable(base, spec.name))
		targetRows := decodeRows(rawTable(target, spec.name))

		td := TableDelta{Table: spec.name}

		for _, id := range sortedIDs(targetRows) {
			if _, ok := baseRows[id]; !ok {
				td.Added = append(td.Added, targetRows[id])
			}
		}
		for _, id := range sortedIDs(baseRows) {
			if _, ok := targetRows[id]; !ok {
				td.Removed = append(td.Removed, baseRows[id])
			}
		}
		for _, id := range sortedIDs(baseRows) {
			baseRow, ok := baseRows[id]
			if !ok {
				continue
			}
			targetRow, ok := targetRows[id]
			if !ok {
				continue
			}
			if fields := diffFields(baseRow, targetRow); len(fields) > 0 {
				td.Changed = append(td.Changed, RowDelta{ID: id, Fields: fields})
			}
		}

		if len(td.Added) > 0 || len(td.Removed) > 0 || len(td.Changed) > 0 {
			d.Tables = append(d.Tables, td)
		}
	}
	return d
}

// rawTable 取 payload 中指定表的行数组 JSON（缺表=空）。
func rawTable(p *Payload, name string) json.RawMessage {
	if p == nil {
		return nil
	}
	return p.Tables[name]
}

// decodeRows 把行数组解码为 id→行 的映射。数值用 json.Number 保真。
func decodeRows(raw json.RawMessage) map[int64]map[string]any {
	out := make(map[int64]map[string]any)
	if len(raw) == 0 {
		return out
	}
	var rows []map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&rows); err != nil {
		return out
	}
	for _, row := range rows {
		out[rowID(row["id"])] = row
	}
	return out
}

// rowID 从 id 字段值解析主键（json.Number/int64/float64 兼容）。
func rowID(v any) int64 {
	switch x := v.(type) {
	case json.Number:
		n, _ := x.Int64()
		return n
	case int64:
		return x
	case float64:
		return int64(x)
	}
	return 0
}

// diffFields 比较两行字段差异（键并集，稳定排序；缺失侧记为 null）。
func diffFields(baseRow, targetRow map[string]any) map[string]FieldDelta {
	keys := make([]string, 0, len(baseRow)+len(targetRow))
	seen := make(map[string]bool, len(baseRow)+len(targetRow))
	for k := range baseRow {
		keys = append(keys, k)
		seen[k] = true
	}
	for k := range targetRow {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var fields map[string]FieldDelta
	for _, k := range keys {
		oldV, oldOK := baseRow[k]
		newV, newOK := targetRow[k]
		if oldOK == newOK && reflect.DeepEqual(oldV, newV) {
			continue
		}
		if !oldOK {
			oldV = nil
		}
		if !newOK {
			newV = nil
		}
		if fields == nil {
			fields = make(map[string]FieldDelta)
		}
		fields[k] = FieldDelta{Old: oldV, New: newV}
	}
	return fields
}

// sortedIDs 升序返回行 id（diff 输出定序）。
func sortedIDs(rows map[int64]map[string]any) []int64 {
	ids := make([]int64, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
