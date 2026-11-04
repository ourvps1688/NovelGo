# MCP 工具开发规范

## 1. 文件结构

- 一个领域一个文件：`xxx_tools.go`
- 公共工具函数放 `utils.go`
- 注册函数必须是独立顶层函数：`func RegisterXxxTools(r *Registry)`
- 文件末尾放 Register 函数

## 2. 工具实现模板

```go
type XxxTool struct{}

func (t *XxxTool) Name() string        { return "xxx" }
func (t *XxxTool) Description() string { return "工具描述，给 LLM 看，说明适用场景和参数用法" }
func (t *XxxTool) Category() ToolCategory { return CategoryNovelManagement }

func (t *XxxTool) JSONSchema() json.RawMessage {
    return SchemaOf(XxxArgs{})
}

func (t *XxxTool) ExposeToLLM() bool { return true }
func (t *XxxTool) NewArgs() any     { return &XxxArgs{} }

func (t *XxxTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
    a := args.(*XxxArgs)
    return &ToolResult{Success: true, Data: ...}, nil
}
```

Args 结构体同时使用两类 tag：

- `jsonschema` tag —— 生成 OpenAI function calling 的 JSON Schema（给 LLM 看）
- `validate` tag —— 运行时校验（给 Registry 用）

```go
type XxxArgs struct {
    Name string `json:"name" jsonschema:"required,description=名称"       validate:"required"`
    Type string `json:"type" jsonschema:"required,enum=a,enum=b,enum=c"  validate:"required,oneof=a b c"`
    Size int    `json:"size" jsonschema:"default=20,minimum=1,maximum=100" validate:"min=1,max=100,omitempty"`
}
```

**校验由 Registry.Execute 统一执行**，工具自身不需要 `json.Unmarshal` 或 `validate.Struct`，直接从 `args.(*XxxArgs)` 取值。

`jsonschema` tag 对照 Pydantic：

| Pydantic | Go jsonschema tag |
|----------|-------------------|
| `Field(description=...)` | `description=xxx` |
| `Literal["a","b"]` | `enum=a,enum=b` |
| `Field(default=...)` | `default=xxx` |
| `Field(ge=1,le=100)` | `minimum=1,maximum=100` |
| 无默认值 → required | `required` |

## 3. 错误处理

| 类型 | 做法 | 返回值 |
|------|------|--------|
| 业务错误（参数不合法、资源不存在、状态冲突） | 返回已知的业务结论；LLM 可换参数或流程处理 | `return &ToolResult{Success: false, Error: "..."}, nil` |
| 意外异常（DB、磁盘、网络） | 包装 error；若部分操作已完成，先写明已确认状态，再包装根因 | `return nil, fmt.Errorf("正文已回退，标题未更新: %w", err)` |
| 参数校验 | 不手写。Registry 统一用 `validate.Struct()` 执行，工具拿到时已是合法值 | — |

`Registry.Execute()` 会将所有返回给 LLM 的错误文本压成单行、最多 300 个字符。业务/参数 `ToolResult.Error` 只保留原文摘要；收到 `err != nil` 时会记录完整错误、返回 `ErrKind: "system"`，且 LLM 看到的是“工具执行失败：”加上状态说明和短原因。工具不需要自行截断，也不要手工构造 system `ToolResult`。

不要按“底层是否使用了 DB”划分：`gorm.ErrRecordNotFound`、可预期的唯一约束或状态冲突都应转换成业务结果；`database is locked`、磁盘满、超时等未知依赖故障必须返回 error。工具不要 `recover()` 包裹 `Execute()` 方法体。

## 4. 工具白名单

- 主 agent 和子 agent 各自持有 `AllowedTools []string`，定义在 agent 配置中
- `Registry.OpenAI(allowed map[string]bool)` 生成 LLM 可见的工具列表
- `Registry.Execute(allowed map[string]bool)` 执行时双重校验——工具不存在 vs 工具不可用
- `allowed=nil` 表示不限制
- 使用 `AllowSet([]string{"a","b"})` 将 []string 转为 map[string]bool，在 agent 配置 init 中调用一次

## 5. Category

| Category | 适用工具 |
|----------|---------|
| `novel_management` | 小说/章节/角色/地点 CRUD |
| `writing_assistant` | 创作辅助（大纲、弧线、时间线、编辑、子Agent） |
| `memory_retrieval` | 检索查询（记忆、角色记忆、时间线、弧线、读者视角） |
| `consistency_check` | 一致性审查 |

仅作组织用途，不驱动不同行为。

## 6. 注册

- `ExposeToLLM()` 默认 `true`
- 新增工具后在 `registry.go` 调用 Register 函数

## 7.格式化
- 返回的消息能格式化成md的就格式化，不要直接返回原始json。方便llm理解
- 图结构可以返回邻接表，而不是原始的点和边。
- 格式化内容中需要被其他工具引用的 ID，统一使用 `[args_json_tag:X]` 格式（如 `[entry_id:5]`、`[relation_id:5]`），与 Args 的 json tag 一致，LLM 可直接填入参数。
- 不需要引用的 ID 不嵌入（如 `get_characters` 的角色 ID —— LLM 已从该工具拿到，无需在格式化中复现）。

## 8. 命名规范

- CRUD 工具统一使用 `create_` / `update_` / `get_` 前缀，禁止 `add_` / `delete_` / `list_`
- 注册函数：`RegisterXxxTools`

## 9. Update 工具的 PATCH 模式

两种场景：

**场景一（大多数）—— Args 是 DB 模型的子集，json tag 一致：**

直接用 `json.Unmarshal(tc.RawArgs, &entity)`，无需手写 if 逐个赋值。

**场景二（特殊逻辑）—— 事务、多步骤、条件分支等：**

直接用 `args.(*XxxArgs)` 手动操作，不套 unmarshal。

---

`tc.RawArgs` 由 Registry.Execute 在调用工具前注入，存放 LLM 传的原始 JSON。

**注意**：Args 中声明为 string 的「JSON 文本」字段（abilities/personality/detail_json/tags/params 等）若被 LLM 传真 JSON 数组/对象，Registry.Execute 会在解码前就地规范化为紧凑 JSON 文本（见 `args_coerce.go`），`tc.RawArgs` 亦为规范化后的字节——故 PATCH 到 entity 的 string 字段时拿到的一定是 JSON 文本，两条解码路径（Args 解码与下方 Unmarshal）都不会因数组/对象参数报 "cannot unmarshal array into … of type string"。

Unmarshal 是 PATCH 语义：JSON 里传了哪些 key 就改哪些字段，未出现的保持原值。

**约束**：Args 只放允许 LLM 自由修改的字段。Finder 字段（`character_id` / `entry_id` / `relation_id` 等）的 json tag 刻意与 DB 模型 PK（`id`）不同名——不是因为 unmarshal 有风险，而是让 json tag 自说明字段用途，LLM 看到 `entry_id` 就知道填什么。

## 10. create 工具 — 循环+事务模式

所有 create 工具统一使用循环+事务写入（原先使用 `db.Create(&[]T)` 批量 INSERT，但因 GORM afterCreate 回调在切片 Dest 下无法逐行提取主键，导致 operation_log 记录失效，故改为循环+事务）：

1. **Item 结构体** — 单条数据的字段，含 `jsonschema` 和 `validate` tag
2. **Args 包裹** — 一个 slice 字段，`validate:"min=1,max=N,dive"`
3. **预校验** — 业务检查放外面，返回 `*ToolResult{Success: false, Error: "..."}, nil`；系统错误返回 `nil, error`
4. **循环+事务** — `db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { for ... { tx.Create(&entity) } })`，保证原子性，失败时返回具体条目原因（如 `创建角色 [alice] 失败: ...`）
5. **返回值** — `{"ids": [...], "count": N}`

工具描述统一为"保证原子性，失败时返回具体条目原因"，不暴露事务实现细节。

预校验 SELECT：
- 单列 IN（如 `WHERE id IN (1,2,3)`）用 batch 查询
- 双列 pair 匹配 GORM 无现成 API，小 N 循环可接受

## 11.代码要求
- 除了查询工具，其余的工具 不再回传llm传过的字段，比如update，llm传过来了要更改哪些字段，不需要再次回传，只需要说明修改成功即可
