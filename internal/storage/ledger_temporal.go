package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// ── temporal/history 写路径（WBS 2.6 / G3）────────────────────────────
//
// 目标（WBS §0.3）：所有台账写操作维护 created_at/updated_at/changed_at + 追加 history[]，
// 并判定「语义变更 vs 格式调整」：
//   - updated_at：任何一次落盘/整理都更新（GORM autoUpdateTime 自动维护，含格式调整）；
//   - changed_at：只在事实真变时更新（本文件在语义 diff 非空时才置位）；
//   - history[]：append-only 变更履历——语义变更逐字段记录 old→new；
//     纯格式调整（仅空白/JSON 等价重排）不记履历、不动 changed_at；
//     create 记一条 (create) 摘要、delete 记一条 (delete) 摘要。
//
// 实现方式：复用/参考 operation_log 的 GORM 回调拦截（RegisterOplogHooks 同款机制），
// 在 DB 层单点覆盖全部台账写入口（mcp_tools 工具、app 视图、store 方法），
// 而不是逐个写点手工埋点——验收「任意台账改动能查到 changed_at + history」由拦截保证。
//
// 覆盖范围：所有带 changed_at 列的台账表（characters/character_relations/locations/
// story_arcs/arc_nodes/time_entries/reader_perspectives 及新表 chronology_anchors/
// character_fact_knowledge/pov_roster 等，按 Schema 动态识别，未来加表自动纳入）。
//
// source 判定（§0.3/§0.4 枚举 auto|author|retcon）：
//   - 显式 WithLedgerSource 标注优先（mcp_tools Registry.Execute 统一标 auto）；
//   - 未标注时按 TurnInfo 判定：agent 回合内=auto，前端 CRUD=author；
//   - 条目归属卷已冻结（volumes.frozen）时升级为 retcon（「卷冻结后对冻结卷条目的改动」）。
//
// 已知边界：
//   - 带 WHERE 的批量 Updates/Delete（无单行 PK 的 Dest）不逐行拦截——存量迁移/整书删除
//     等旁路本就不要求履历；单条目写路径（Save/Create/Delete(&entity)）全覆盖；
//   - changed_in_volume_id/changed_in_chapter_id 与 history 的 volume_id/chapter_id 为
//     「变革定位」，写入口暂无卷/章上下文（ToolContext 无此信息），保持 NULL 不猜。

// 变更履历的摘要字段名（per-field 记录用真实列名，create/delete 用摘要伪字段）。
const (
	HistoryFieldCreate = "(create)"
	HistoryFieldDelete = "(delete)"
)

// ledgerSkipColumns 是 diff 跳过的身份/时间戳列：身份列不可变，时间戳列由机制自身维护。
var ledgerSkipColumns = map[string]bool{
	"id":                    true,
	"novel_id":              true,
	"created_at":            true,
	"updated_at":            true,
	"changed_at":            true,
	"changed_in_volume_id":  true,
	"changed_in_chapter_id": true,
}

// ledgerLabelFields 是 create/delete 摘要取值的候选列（按表结构选第一个非空）。
var ledgerLabelFields = []string{"Name", "Title", "Content", "RelationDescribe"}

// ── source 上下文 ────────────────────────────────────────────────────

type ledgerSourceKeyType struct{}

// WithLedgerSource 显式标注后续台账写操作的 history.source
// （HistorySourceAuto/Author/Retcon）。mcp_tools 统一标 auto；未标注时按 TurnInfo 判定。
func WithLedgerSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, ledgerSourceKeyType{}, source)
}

// ledgerSourceFromContext 解析基础 source（未做冻结卷 retcon 升级）。
func ledgerSourceFromContext(ctx context.Context) string {
	if s, ok := ctx.Value(ledgerSourceKeyType{}).(string); ok && s != "" {
		return s
	}
	if _, ok := getTurnInfo(ctx); ok {
		return HistorySourceAuto
	}
	return HistorySourceAuthor
}

// ── 履历时钟 ─────────────────────────────────────────────────────────

var (
	ledgerAtMu   sync.Mutex
	ledgerAtLast time.Time
)

// nextLedgerAt 返回严格递增的履历时刻。
// ledger_change_history 的 (entry_table, entry_id, at) 是唯一索引——同一事件多字段、
// 同一纳秒内的连续事件都可能撞同一时间戳，用进程内单调时钟保证永不撞索引。
func nextLedgerAt() time.Time {
	ledgerAtMu.Lock()
	defer ledgerAtMu.Unlock()
	now := time.Now()
	if !now.After(ledgerAtLast) {
		now = ledgerAtLast.Add(time.Nanosecond)
	}
	ledgerAtLast = now
	return now
}

// ── 回调注册 ─────────────────────────────────────────────────────────

// RegisterLedgerTemporalHooks 在 *gorm.DB 上注册台账 temporal/history 写路径回调。
// 调用时机：storage.Open + migrate.Run 之后（与 RegisterOplogHooks 并列）。
// 钩子对非台账表（无 changed_at 列）完全无感，也不互相递归（ledger_change_history
// 自身无 changed_at，写履历不会再触发台账回调）。
func RegisterLedgerTemporalHooks(db *gorm.DB) error {
	type hook struct {
		name string
		reg  func() error
	}
	hooks := []hook{
		{"ledger_temporal:before_create", func() error {
			return db.Callback().Create().Before("gorm:before_create").Register("ledger_temporal:before_create", ledgerBeforeCreate)
		}},
		{"ledger_temporal:after_create", func() error {
			return db.Callback().Create().After("gorm:after_create").Register("ledger_temporal:after_create", ledgerAfterCreate)
		}},
		{"ledger_temporal:before_update", func() error {
			return db.Callback().Update().Before("gorm:before_update").Register("ledger_temporal:before_update", ledgerBeforeUpdate)
		}},
		{"ledger_temporal:after_update", func() error {
			return db.Callback().Update().After("gorm:after_update").Register("ledger_temporal:after_update", ledgerAfterUpdate)
		}},
		{"ledger_temporal:before_delete", func() error {
			return db.Callback().Delete().Before("gorm:before_delete").Register("ledger_temporal:before_delete", ledgerBeforeDelete)
		}},
		{"ledger_temporal:after_delete", func() error {
			return db.Callback().Delete().After("gorm:after_delete").Register("ledger_temporal:after_delete", ledgerAfterDelete)
		}},
	}
	for _, h := range hooks {
		if err := h.reg(); err != nil {
			return fmt.Errorf("register %s: %w", h.name, err)
		}
	}
	return nil
}

// ledgerSchema 返回台账表 Schema（带 changed_at 列），非台账表返回 nil。
func ledgerSchema(db *gorm.DB) *schema.Schema {
	sch := db.Statement.Schema
	if sch == nil || db.Statement.Dest == nil {
		return nil
	}
	if sch.LookUpField("ChangedAt") == nil {
		return nil
	}
	return sch
}

// destValue 把 Statement.Dest 规整为可寻址的 struct Value。
func destValue(db *gorm.DB) (reflect.Value, bool) {
	v := reflect.ValueOf(db.Statement.Dest)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	return v, v.Kind() == reflect.Struct
}

// ── Create 回调 ─────────────────────────────────────────────────────

// ledgerBeforeCreate 建行即置 changed_at=now（新建条目不留 NULL，temporal 从创建起有值）。
func ledgerBeforeCreate(db *gorm.DB) {
	sch := ledgerSchema(db)
	if sch == nil {
		return
	}
	setLedgerChangedAt(db, sch)
}

// ledgerAfterCreate 追加一条 (create) 摘要履历。
func ledgerAfterCreate(db *gorm.DB) {
	sch := ledgerSchema(db)
	if sch == nil {
		return
	}
	row, ok := destValue(db)
	if !ok {
		return
	}
	entryID, ok := ledgerInt64(db, sch, row, "ID")
	if !ok {
		return
	}
	appendLedgerHistory(db, sch, row, entryID, []ledgerChange{
		{field: HistoryFieldCreate, newValue: ledgerLabel(db, sch, row)},
	})
}

// ── Update 回调 ─────────────────────────────────────────────────────

// ledgerBeforeUpdate 计算语义 diff：
//   - 空 diff（无改动或纯格式调整）→ 不动 changed_at、不记履历（updated_at 照常更新）；
//   - 有语义变更 → changed_at=now，逐字段变更暂存给 after_update 记履历。
func ledgerBeforeUpdate(db *gorm.DB) {
	sch := ledgerSchema(db)
	if sch == nil {
		return
	}
	oldRow := fetchOldRow(db)
	if oldRow == nil {
		return // 批量/无 PK Dest 不拦截（见文件头「已知边界」）
	}
	oldV := reflect.ValueOf(oldRow)
	for oldV.Kind() == reflect.Pointer {
		oldV = oldV.Elem()
	}
	newV, ok := destValue(db)
	if !ok {
		return
	}

	changes := ledgerDiff(db, sch, oldV, newV)
	if len(changes) == 0 {
		return
	}
	// 钉住护栏（R2b-7）：已钉住条目的语义修改，auto 写拒收（需走 retcon 级审批）。
	// AddError 后 gorm:update 回调（if db.Error != nil { return }）不再执行 SQL——真拦截。
	if entryID, ok := ledgerInt64(db, sch, newV, "ID"); ok {
		if err := guardPinnedAuto(db, sch.Table, entryID, db.Statement.Context); err != nil {
			db.AddError(err)
			return
		}
	}
	setLedgerChangedAt(db, sch)
	db.InstanceSet("ledger_temporal:changes", changes)
}

func ledgerAfterUpdate(db *gorm.DB) {
	sch := ledgerSchema(db)
	if sch == nil {
		return
	}
	raw, ok := db.InstanceGet("ledger_temporal:changes")
	if !ok {
		return
	}
	changes, _ := raw.([]ledgerChange)
	if len(changes) == 0 {
		return
	}
	row, ok := destValue(db)
	if !ok {
		return
	}
	entryID, ok := ledgerInt64(db, sch, row, "ID")
	if !ok {
		return
	}
	appendLedgerHistory(db, sch, row, entryID, changes)
}

// ── Delete 回调 ─────────────────────────────────────────────────────

func ledgerBeforeDelete(db *gorm.DB) {
	sch := ledgerSchema(db)
	if sch == nil {
		return
	}
	oldRow := fetchOldRow(db)
	if oldRow == nil {
		return
	}
	// 钉住护栏（R2b-7）：删除=语义修改，已钉住条目 auto 删拒收（需走 retcon 级审批）。
	oldV := reflect.ValueOf(oldRow)
	for oldV.Kind() == reflect.Pointer {
		oldV = oldV.Elem()
	}
	if oldV.IsValid() {
		if entryID, ok := ledgerInt64(db, sch, oldV, "ID"); ok {
			if err := guardPinnedAuto(db, sch.Table, entryID, db.Statement.Context); err != nil {
				db.AddError(err)
				return
			}
		}
	}
	db.InstanceSet("ledger_temporal:delete_old", oldRow)
}

// ledgerAfterDelete 追加一条 (delete) 摘要履历（append-only，删了的条目仍可回溯）。
func ledgerAfterDelete(db *gorm.DB) {
	sch := ledgerSchema(db)
	if sch == nil {
		return
	}
	raw, ok := db.InstanceGet("ledger_temporal:delete_old")
	if !ok || raw == nil {
		return
	}
	oldV := reflect.ValueOf(raw)
	for oldV.Kind() == reflect.Pointer {
		oldV = oldV.Elem()
	}
	if !oldV.IsValid() {
		return
	}
	entryID, ok := ledgerInt64(db, sch, oldV, "ID")
	if !ok {
		return
	}
	appendLedgerHistory(db, sch, oldV, entryID, []ledgerChange{
		{field: HistoryFieldDelete, oldValue: ledgerLabel(db, sch, oldV)},
	})
}

// ── diff 与序列化 ────────────────────────────────────────────────────

// ledgerChange 是单字段的语义变更（history[] 一条记录）。
type ledgerChange struct {
	field, oldValue, newValue string
}

// ledgerDiff 逐字段比较旧行/新行，只返回语义变更（格式调整被归一化吸收）。
func ledgerDiff(db *gorm.DB, sch *schema.Schema, oldRow, newRow reflect.Value) []ledgerChange {
	var out []ledgerChange
	for _, f := range sch.Fields {
		if ledgerSkipColumns[f.DBName] {
			continue
		}
		oldStr := ledgerStringify(fieldValue(db, f, oldRow))
		newStr := ledgerStringify(fieldValue(db, f, newRow))
		if ledgerValuesEqual(oldStr, newStr, f.FieldType.Kind() == reflect.String) {
			continue
		}
		out = append(out, ledgerChange{field: f.DBName, oldValue: oldStr, newValue: newStr})
	}
	return out
}

// ledgerValuesEqual 判定「同一语义」：字符串 TrimSpace 归一（空白/排版=格式调整），
// 双方可解析 JSON 时按解析值比较（键序/空白重排=格式调整）；其余按序列化文本精确比较。
func ledgerValuesEqual(oldStr, newStr string, isString bool) bool {
	if oldStr == newStr {
		return true
	}
	if !isString {
		return false
	}
	if strings.TrimSpace(oldStr) == strings.TrimSpace(newStr) {
		return true
	}
	if json.Valid([]byte(oldStr)) && json.Valid([]byte(newStr)) {
		var a, b any
		if json.Unmarshal([]byte(oldStr), &a) == nil && json.Unmarshal([]byte(newStr), &b) == nil {
			return reflect.DeepEqual(a, b)
		}
	}
	return false
}

// ledgerStringify 把字段值序列化为履历文本（指针解引用，nil→""）。
func ledgerStringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case time.Time:
		return x.Format(time.RFC3339Nano)
	}
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return ""
	}
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		if rv.Bool() {
			return "true"
		}
		return "false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", rv.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%v", rv.Float())
	}
	if t, ok := rv.Interface().(time.Time); ok {
		return t.Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("%v", rv.Interface())
}

func fieldValue(db *gorm.DB, f *schema.Field, row reflect.Value) any {
	v := f.ValueOf
	if v == nil {
		return nil
	}
	val, _ := v(db.Statement.Context, row)
	return val
}

// ledgerInt64 取行上的 int64 列（主键/NovelID）。
func ledgerInt64(db *gorm.DB, sch *schema.Schema, row reflect.Value, name string) (int64, bool) {
	f := sch.LookUpField(name)
	if f == nil {
		return 0, false
	}
	val := ledgerStringify(fieldValue(db, f, row))
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ledgerPtrInt64 取行上的 *int64 列（VolumeID 等）。
func ledgerPtrInt64(db *gorm.DB, sch *schema.Schema, row reflect.Value, name string) *int64 {
	f := sch.LookUpField(name)
	if f == nil {
		return nil
	}
	raw := fieldValue(db, f, row)
	rv := reflect.ValueOf(raw)
	if !rv.IsValid() {
		return nil
	}
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Int64 {
		return nil
	}
	n := rv.Int()
	return &n
}

// ledgerLabel 取 create/delete 摘要文本（首个非空的 Name/Title/Content/RelationDescribe）。
func ledgerLabel(db *gorm.DB, sch *schema.Schema, row reflect.Value) string {
	for _, name := range ledgerLabelFields {
		f := sch.LookUpField(name)
		if f == nil {
			continue
		}
		s := ledgerStringify(fieldValue(db, f, row))
		if s == "" {
			continue
		}
		return truncateRunes(s, 120)
	}
	return ""
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ── 写入与 source 判定 ───────────────────────────────────────────────

// setLedgerChangedAt 置 changed_at=now（*time.Time 列）。
func setLedgerChangedAt(db *gorm.DB, sch *schema.Schema) {
	f := sch.LookUpField("ChangedAt")
	if f == nil || f.Set == nil {
		return
	}
	row, ok := destValue(db)
	if !ok {
		return
	}
	now := nextLedgerAt()
	if err := f.Set(db.Statement.Context, row, &now); err != nil {
		db.AddError(fmt.Errorf("ledger changed_at: %w", err))
	}
}

// appendLedgerHistory 逐字段追加 ledger_change_history（append-only）。
// 失败用 AddError 上抛（与 operation_log 一致：审计写失败不应静默丢失）。
func appendLedgerHistory(db *gorm.DB, sch *schema.Schema, row reflect.Value, entryID int64, changes []ledgerChange) {
	if len(changes) == 0 {
		return
	}
	novelID, _ := ledgerInt64(db, sch, row, "NovelID")
	source := resolveLedgerSource(db, ledgerPtrInt64(db, sch, row, "VolumeID"),
		ledgerSourceFromContext(db.Statement.Context))
	for _, c := range changes {
		rec := LedgerChangeHistory{
			NovelID:    novelID,
			EntryTable: sch.Table,
			EntryID:    entryID,
			Field:      c.field,
			Old:        c.oldValue,
			New:        c.newValue,
			Source:     source,
			At:         nextLedgerAt(),
		}
		if err := db.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Create(&rec).Error; err != nil {
			db.AddError(fmt.Errorf("ledger history write: %w", err))
			return
		}
	}
}

// resolveLedgerSource 冻结卷升级：条目归属卷（volume_id）已冻结时 source 强制 retcon
// （§0.3「卷冻结后对冻结卷条目的改动，source 自动标 retcon」）。
// volume_id=NULL=无卷/全书级，不升级；查询失败（表缺失等）保守回落基础 source。
func resolveLedgerSource(db *gorm.DB, volumeID *int64, base string) string {
	if volumeID == nil {
		return base
	}
	var frozen bool
	row := db.Session(&gorm.Session{NewDB: true, SkipHooks: true}).
		Raw("SELECT frozen FROM volumes WHERE id = ?", *volumeID).Row()
	if row == nil {
		return base
	}
	if err := row.Scan(&frozen); err != nil {
		return base
	}
	if frozen {
		return HistorySourceRetcon
	}
	return base
}
