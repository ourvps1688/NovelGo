# MCP Client 接入设计（外部 MCP server 消费）

> 状态：设计稿暂存，未实现。本文记录"外部 agent 经 MCP 调用 NovelGo 工具"的反向设计——**NovelGo 作为 MCP client，消费外部 MCP server 的工具**。

## 设计目标

- 让 NovelGo 的 agent 能调用外部 MCP server（如生图、知识库、云服务）提供的工具，而不把外部工具塞进 `tools` 数组。
- **tools 数组保持稳定**：不改 tools 定义，缓存不因外部工具增删失效。
- 结构化的保证来自"原生 function calling 调稳定包装工具"，外部工具的 schema 按需由 agent 用 `Read` 读取。

## 核心思想：包装工具 + 磁盘 schema（复刻 Trae 自身模式）

Trae 环境就是这种形态的完整落地：

- LLM 的 tools 数组只有少量稳定工具（含一个通用路由 `run_mcp`），内层工具的 schema 全在请求之外；
- 真实工具的 descriptor 是磁盘上的 JSON 文件，agent 用 `Read`/`LS` 按需拉取；
- 发起调用时 agent 对 `run_mcp` 发原生 tool_call，内层 args 由客户端路由执行。

NovelGo 照搬这套，两层：

```
LLM 视角:
  tools 数组:  [call_mcp_tool, ...现有内置工具, Read, LS...]   ← 稳定，无外部工具
  system/user 消息: 已启用外部工具的 {tool_id, name, description} 轻量目录
  AI: 看目录 → 决定用哪个 → Read 读 schemas/<server>/<tool>.json
      → 按 schema 拼 args → call_mcp_tool(tool_id, args)

客户端视角:
  call_mcp_tool → 按 tool_id 拆 server → stdio/HTTP JSON-RPC tools/call
      → 结果归一化为 ToolResult → 作为 tool 消息回灌 LLM
```

## 具体设计

### 1. 稳定包装工具（唯一新增的 Registry 工具）

```
call_mcp_tool
  args: { tool_id: string, args: object }   // tool_id = "mcp:{server}:{tool}"
```

按普通 Registry 工具实现。`Execute` 内不校验具体字段结构——args 是自由 JSON，由客户端按真实 schema 校验（见 §5）。

### 2. 外部工具目录（name + description）

- 已启用 server 的工具，只把 `tool_id` + `name` + `description` 拼进 system prompt 或一条 user 消息（轻量目录，供模型选工具）。
- **禁止把外部工具注入 tools 定义**（那会破坏数组稳定性 + 缓存）。
- 目录随"启用的 server 集合"构建，配置变更是会话级稀有事件。

### 3. schema 按需读取（不新增专用工具）

- 连接 server 时客户端调 `tools/list` 拉全量 schema，**落盘**为 `{DataDir}/schemas/{server}/{tool}.json`（复刻 Trae 的 descriptor 文件模式）。
- agent 需要时直接用现有 **Read 工具**读该文件，按字段名/类型/必填拼 args。
- 读过的 schema 进 tool result 后留在会话历史，同一工具二次使用不重复读。

### 4. 连接管理（MCP client 本体）

- 传输：stdio（本地 spawn 子进程）或 HTTP/SSE（远程服务），用现成 SDK（如 `mark3labs/mcp-go`）封装 initialize 握手、tools/list、tools/call、会话生命周期。
- 连接建立时同步拉 schema 落盘 + 生成目录；`tools/list` 结果按协议 `ttlMs` 缓存。

### 5. 与现有 Registry 的集成点

- `call_mcp_tool` 是普通工具，走现有 `OpenAI()` / `Execute()` 链路，白名单、展示、失败中断复用。
- **动态 args 校验**：`Execute` 里对 `args` 做 schema 级校验（用落盘的 JSON Schema 校验器），不走 `validate.Struct` 强类型路径——这是唯一动 Registry 核心的点，需保证不破坏现有 30+ 工具的强类型校验。
- 命名空间 `mcp:{server}:{tool}` 避免与内置工具撞名。

### 6. 护栏

- **默认关闭**：外部 server 显式启用才注入目录，不进默认 prompt。
- **白名单**：`OpenAI(allowed)` 按 agent 类型过滤，外部工具按需放行。
- **写操作过审批**：外部写类工具复用 `EmitApproval` 审批流。
- **错误映射**：连接层错误 → `ErrKindSystem`；工具自身错误 → `ErrKindBusiness`。

## 方案定稿

**只做懒加载**，不考虑全量注入：外部工具的 schema 一律不进 `tools` 定义、不整体拼进 user 消息，按需由 agent 用 `Read` 读取 `schemas/{server}/{tool}.json` 后经 `call_mcp_tool` 调用。
