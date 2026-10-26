# NovelGo 技术状态与演进护栏（Technical Status & Upgrade Guardrails）

> **文档目的**：本文件是 NovelGo 项目的「技术状态快照 + 待办优先级 + 升级护栏」。
> 它解决一个核心风险——**上下文丢失导致后续优化/升级跑偏**：未来某次会话或新 clone
> 若只凭片段记忆动手，可能推翻已有的正确决策、踩已踩过的坑、或重做已否决的方案。
> 本文件把「已知事实 / 关键决策 / 红线 / 待办」固化下来，作为对齐基准。
>
> - **最后更新**：2026-10-16
> - **流程与协作规则**见根目录 `AGENTS.md`（本文不重复，只补状态与决策）。
> - **任何一次重大改动后，请同步更新本文件对应小节**，保持它始终是当前事实的镜像。

---

## 1. 项目定位与合规底线（不可动摇）

- NovelGo = `sigpanic/goink`（Go + Wails v2 + React 19 + SQLite/ONNX）的合规派生 fork，AGPL-3.0 + NOTICE 附加条款。
- **硬性合规**（缺失会导致 license 违规）：
  - 必须保留 `Based on Goink by 响滩 (sigpanic) — https://github.com/sigpanic/goink`。
  - 必须保留 `This is a modified version of Goink...` 声明，出现在 README / NOTICE / About 对话框。
  - **不得**移除或暗示原作者背书。
- 模块路径：`github.com/novelgo/novelgo`。
- 数据目录：`~/NovelGo`（Windows 为可执行文件附近）；小说元数据文件 `novelgo.md`。
- 品牌 mark：N+G 字母组合 monogram（**用户明确不要书的意象**）。
- Git：origin = `ourvps1688/NovelGo`，upstream = `sigpanic/goink`；历史已 unshallow。

---

## 2. 技术栈与版本锁定矩阵（升级前先对齐）

| 项 | 当前值 | 约束 / 坑 |
|---|---|---|
| Go | **1.25.0** (`go.mod`) | CI `setup-go` 也锁 1.25 |
| Wails 库 | **v2.16.0** (`go.mod`) | 与本地 dev CLI 一致 |
| Wails CLI（本地 dev） | **v2.16.0** | `wails dev` 会把 go.mod 的 wails 版本改写为 CLI 版本 |
| Wails CLI（**release.yml** + verify-macos-codesign.yml） | **v2.16.0**（已对齐，2026-10-16） | 与库/本地 CLI 一致；P1 已完成 |
| React | 19 | + TypeScript + Tailwind 4 + shadcn/ui |
| 构建 | CGO + SQLite + ONNX | 见 §4 构建环境 |
| 图标/绑定 | wails 自动生成 `wailsjs/` | **禁止手改**，只能 `wails generate module` 重生成 |

**⚠️ Wails 版本一致性是头号护栏**：v2.16 相比 v2.12 给含嵌套结构体的类（如 `ChatInput`、
`SaveAuthorAliasesInput`）生成 `convertValues` 方法。若 release 用 2.12 CLI 生成绑定，而本地
dev 用 2.16 验证并提交绑定，发布件与 dev 测试产物会分叉 → 潜在编译/运行时不一致。
**已完成：release.yml 三处 + verify-macos-codesign.yml 一处 `wails@v2.12.0` → `wails@v2.16.0`**（2026-10-16，状态见 §5）。

---

## 3. 当前已完成能力（as of 2026-10-16，已验证）

| 能力域 | 状态 | 关键事实 |
|---|---|---|
| 技能意图路由 | ✅ 完成 | `useWhen` 结构化 + 确定性 cue 路由 + `RouteUserMessageSmart` LLM 兜底（4s 超时 / 0.6 置信 / 失败降级） |
| 路由 CI 护栏 | ✅ 完成 | `internal/skill/regression_test.go`（确定性，零 key）+ `.github/workflows/skill-fallback-eval.yml`（夜跑 LLM，需 `DEEPSEEK_API_KEY` secret） |
| **「签约」智能层** | ✅ 完成且验证 | 3 个专项技能：`signability-audit`（manual，七维体检）、`story-blueprint`/`worldbuilding-design`（auto）；已用真实 deepseek 验证产出「七维星级表 + 致命伤清单」结构化体检（非空壳）。**新增编排层** `signability-workflow`（manual，系统类）——把 3 专项技能串成「现状诊断→规划蓝图→世界设定→写作推进→签约体检→闭环回修」引导式闭环（2026-10-16，见 §5 P2-3）；**该编排技能后已退役删除**（2026-10-18 起入口改由流水线状态机驱动，见 §9 变更记录） |
| 编辑器体验 | ✅ 完成 | 墨色主题模块 `monacoTheme.ts`；正文字体/行距/关闭行号/关闭 overview ruler；diff 红调淡；滚动条中性灰 |
| 人物关系图可视化 | ✅ 完成 | 后端 `GetCharacterRelations` + 前端 `CharacterGraph`（@antv/g6 v5 dagre 树布局、点击详情卡、懒加载）；F-5/F-6 特性已入库 |
| 数据目录迁移 | ✅ 完成 | `platform.readConfiguredDataDir()` 让 `config.json:data_dir` 生效；`internal/datamove` 安全迁移（同盘 rename/跨盘拷贝+回退）；`app.PickDirectory/UpdateDataDir` + 前端「更改」按钮+进度条+非空确认框 |
| 内置技能总数 | ✅ 26 个 | 含 3 个签约专项技能（`signability-audit`/`story-blueprint`/`worldbuilding-design`）；原签约编排技能 `signability-workflow` 已退役删除（2026-10-16 时点为 19 个）；题材/偏好层在前端 `genreTemplates.ts`（30+ 题材卡） |
| 测试基建 | ✅ 完成 | 55 Go 测试包 + 33 前端测试 + `internal/e2e`（3 OS Go 集成测试，覆盖 novel/chapter/git/ONNX 主链路） |
| 发布链路 | ✅ 完成 | Win(Inno)/Linux(AppImage)/macOS(DMG + codesign + spctl + dmg 挂载验证)，按 `v*` tag 触发 |

---

## 4. 构建与运行环境（踩坑记录）

- **CGO 构建**（Windows）：需 `CGO_CFLAGS=-IC:/tools/sqlite3-headers`（该目录由
  `mattn/go-sqlite3@v1.14.44` 的 `sqlite3-binding.h` 复制而来），且 `PATH` 加
  `C:/tools/mingw64/bin`（winlibs GCC 16.2.0）。新 shell 不继承 User PATH，必须显式导出。
- **沙箱外网**：当前沙箱 Bash 外网被拦；需要联网（deepseek API / GitHub API）时
  `dangerouslyDisableSandbox: true`。`api.deepseek.com` 可达。
- **wails dev 改前端源码**：后台 `npm run dev` 仅服务浏览器实时 HMR，**webview 加载的是
  `frontend/dist` 构建产物**（带 hash 固定文件名），不是 vite 实时源码。改前端后必须
  `npm run build` 并**重启 `wails dev`** 才在桌面窗口生效。
- **LLM 配置**：`C:/Users/Administrator/.novelgo/llm_config.enc`（462 字节，AES-256，
  代码内硬编码 `appKey` 解密，**无外部口令**）。测试可经 `llm.LoadUserConfig` 读到真实 key。
- **git 推送**：SSH config（`C:\Users\Administrator\.ssh\config`，Host github.com →
  ssh.github.com:443，IdentityFile `id_ed25519_novel2all`）。命令：
  `GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`。
- **PAT**：根目录 `github_pat.txt`，已被 `.gitignore` 忽略，**绝不入库**。
- **autocrlf=true**：检出为 CRLF，`gofmt -l` 会全量误报，**勿据此改格式**；提交前只对真实
  改动文件 `gofmt -w`。

---

## 5. 待办优先级（防跑偏：按此顺序推进）

### P1 — 发布正确性（静默风险，建议最先修）
- **[x] Wails 版本对齐**：`release.yml` 三处 + `verify-macos-codesign.yml` 一处 `wails@v2.12.0` → `wails@v2.16.0`（共 4 处）。
  理由与风险见 §2。改完跑 `go build ./...` + `go test ./...`（checks.yml 等价）全绿。
  状态：✅ 已完成（2026-10-16）。额外发现 `verify-macos-codesign.yml:39` 同样 pin 2.12.0，已一并对齐以守住 §6.1 版本一致性护栏。

### P2 — 功能补全 / 产品化
- **[x] 人物关系图可视化**：**已实现并入库**（F-5 虚拟滚动 + F-6 G6 增量更新，提交 `3fc049d`/`02faead`）。
  后端 `GetCharacterRelations`（`app/character_view.go:26`，有 `TestGetCharacterRelations` 守护）→ wails 绑定生成
  → `useCharacterRelations` hook → `CharacterGraph.tsx`（@antv/g6 v5，dagre 树布局；节点点击弹角色详情卡、
  fitView/刷新按钮、loading/error/empty 态、主题感知色）→ `CharacterListView` 的 "graph" 标签页**懒加载**。
  G6 已按阶段2 F-2 在 `vite.config.ts` 配 `manualChunks` 懒加载。
  ⚠️ **文档此前写的「前端没有关系图 UI」为漏扫误判，现已纠正**；`internal/character/types.go:46` 的
  「后续可实现前端可视化关系图」TODO 亦已过时（图已存在），可择机清掉该注释。
- **[x] 数据目录迁移**：**已实现为真正可用的功能**（2026-10-16）。
  此前是残骸态——`~/.novelgo/config.json` 的 `data_dir` 是死字段、前端只读输入框、`UpdateDataDir` 是空壳。
  现做成端到端可用：
  - `internal/platform/runtime.go`：`resolveDataDir()` 在 `NOVELGO_DATA_DIR`、testing 兜底**之前**读取
    `config.json` 的 `data_dir`（`readConfiguredDataDir()`，归一化为绝对路径），让配置真正生效。
  - `internal/datamove/datamove.go`（新包，含 `datamove_test.go`）：`Relocate(src,dst,opts)` 同盘 `os.Rename`、
    跨盘递归拷贝+校验+删源；目标非空未授权返回 `ErrDstNotEmpty`；源/目标互相嵌套返回 `ErrNestedPath`；
    进度经 `Progress` 回调上报（已处理/总字节）。
  - `app/app_config.go`：`PickDirectory()` 弹原生目录框；`UpdateDataDir(path,force)` 重写为
    归一化→相等/嵌套校验→关 DB→`Relocate`→`config.Save`→`platform.ResetDataDirCache()`→`config.Load`→
    `initWithConfig`；失败回退 `restoreDataDir`；进度经 `EventsEmit("data:relocate-progress",{done,total})`。
  - 前端 `GeneralConfigTab.tsx`：数据目录行加「更改」按钮→`PickDirectory`→`UpdateDataDir(dir,false)`；
    捕获 `ErrDstNotEmpty`（"目标目录非空"）弹 `ConfirmDialog`（danger）让用户决定是否 `force=true` 重试；
    迁移中显示 `Loader2` 转圈 + 进度条（订阅 `data:relocate-progress`）；成功/失败 `toast`。i18n 新增
    `dataDir*` 一组键（zh-CN/en 双语）。
  - wails 绑定：`PickDirectory():Promise<string>`、`UpdateDataDir(arg1,arg2):Promise<void>` 已重生成。
  - 门禁：`go build ./...` + `go test ./internal/datamove/... ./internal/platform/... ./app/...` +
    `prettier --check` + `npm run i18n:check` + `npm run build` 全绿。
  - ⚠️ **`internal/character/types.go:46` 的「后续可实现前端可视化关系图」TODO 已过时**（图早已存在），可择机清掉。
- **[x] 「签约」层产品化 —— 技能内核（编排层）**：已实现并入库（`signability-workflow` 内置技能，2026-10-16）；**后已退役删除**（2026-10-18 起由流水线状态机承接，见 §9 变更记录）。
  把 `story-blueprint`/`worldbuilding-design`/`signability-audit` 三专项技能编排为引导式「签约潜力工作流」
  （现状诊断→规划蓝图→世界设定→写作推进→签约体检→闭环回修六阶段，全程服从题材卡约束，只编排不落库）。
  经 NovelGo 真实解析器验证可加载、`internal/skill` 包测试通过、AC1–AC9 全满足、Phase 4 QA 零缺陷。
  工作区 `skill-studio/20261016-135655-signflow/`（需求/设计/源码/QA 全归档）。
  - **[x] 「签约」层产品化 —— 专属 UI 入口**：已于 2026-10-16 实现并入库。应用左侧 ActivityBar 新增「签约潜力工作流」面板
    （`PanelId="signability"`），由 `SignabilityWorkflowView` 承载六阶段导览卡 + 一键启动按钮：
    - 启动 = **合并式启用 `signability-workflow`（SetEnabledSkills 常驻注入）+ 经 `useChatLaunchStore` 通道向 ChatPanel 发 kickoff 消息**。
      关键点：`signability-workflow` 是 `mode: manual`，不进技能目录路由（`ListMetaForCatalog` 仅收 `ModeAuto`），故必须「先启用再发消息」才能启动，而非依赖 useWhen 目录路由。
      注（2026-10-26）：以上为历史机制——PR3 起该入口由 pipeline 状态机驱动（`SignabilityWorkflowView` 直接 `StartPipeline`），不再经 chat skill 启动；`signability-workflow` 已退役删除。
    - 零侵入通道：新建极小 zustand store `useChatLaunchStore`（register/unregister/send），ChatPanel 挂载时 `register(handleSend)`、卸载 `unregister`；入口面板取 `send` 直接发消息，不改 ChatInput/ChatPanel 既有 props 契约。
    - 守卫：`novelId===0` 禁用并提示「先选小说」；未配模型（`selected_model_key` 指向模型不存在）禁用并提示「先配模型」。
    - i18n：新增 `shell.signability` 与 `signability.*` 双语键（标题/副标题/六阶段目标与完成判据/各阶段调用技能/kickoff/启动按钮/禁用提示）。
    - 门禁：`prettier --check` + `npm run i18n:check` + `tsc -b` + `npm run build` 全绿。

### P3 — 清理 / 技术债
- **[ ] `timeline/types.go` 的 `related_entry_ids`**：Python 写入但从未被查询（纯占位数据）。
- **[ ] `WorkspaceView.tsx:199`**：`invalidateQueries` 临时方案，等 3.3 mutation 建好后由
  `onSuccess` 接管（重构债）。
- **[ ] README 补路线图/已知问题小节**（文档小写）。

---

## 6. 升级 / 优化护栏（红线与已知坑）

> 下列各条是「做了会跑偏」或「踩过一次」的约束。动手前先读一遍。

1. **版本一致性**：Wails 库 / 本地 CLI / release CLI 三者必须同为 v2.16.0（P1 修完后）。
2. **wailsjs 绑定**：自动生成产物，**禁止手改**；调用嵌套结构体类必须用 `X.createFrom({...})`
   构造实例（v2.16 的 `convertValues` 约定）。dev 拉起后会重新生成，以重生成结果为准入库。
3. **前端提交前硬闸门**：`npx prettier --check "src/**/*.{ts,tsx}"` 与 `npm run i18n:check`
   必须全绿。`i18n:check` 扫描源文件硬编码中文——**所有中文 UI 文案必须进 i18n locales**，
   否则报红（eslint 不限 warnings 数，0 error 即过）。
4. **前端改动生效路径**：改源码 → `npm run build` → 重启 `wails dev` → webview 才生效
   （见 §4）。不要误以为 vite HMR 直接进了桌面窗口。
5. **CGO/构建环境**：Windows 构建务必带 `CGO_CFLAGS` + `mingw64` PATH（见 §4），否则 CGO 编译失败。
6. **不重做已否决的方案**（见 §7）：不要删 i18n、不要裁 Monaco worker。
7. **单例约束**：ONNX embedder / VectorStore / RefreshQueue 是全局 singleton，**勿引入第二个实例**。
8. **角色关系 append-only**：更新新增记录并置 `is_current=false` 旧行，勿就地覆盖。
9. **数据语义**：`target_chapter` 只用于排序，不当精确过滤条件；消息 append-only，区分
   API/前端/完整审计查询路径。
10. **范围纪律**：未经用户明确允许，不主动扩大任务范围、不自动生成提交/发布/远程操作
    （详见 `AGENTS.md`）。

---

## 7. 已知「伪缺口」（已决策不做，勿重议）

防止未来会话把资源浪费在已被否决的方向上：

- **删 i18n / 只留中文**：评估结论——i18n 全移除最多省 ~100KB（<0.5% dist），而 dist 真正大头是
  Monaco(~12MB) 与 G6(1.4MB)。收益是舍入误差，且会失去用户主动可选的英文能力。**不做**。
- **裁 Monaco ts/css/html worker（省 ~8MB）**：用户明确决策——桌面端大一点也无所谓，**不做**。
  （注：真要减体积的正确方向是 Monaco/G6 懒加载，量级远高于此，但当前未排期。）
- **三签约技能是空壳？**：否。已用真实 deepseek 验证产出结构化七维体检 + 致命伤清单，命中维度 9/9。
- **「只保证写得不差、不保证能签」的旧缺口**：已被 `signability-audit` / `story-blueprint` /
  `worldbuilding-design` 三个技能补上，并接入路由与 CI。

---

## 8. 环境与凭证参考

| 项 | 值 / 位置 | 备注 |
|---|---|---|
| LLM 配置（加密） | `C:/Users/Administrator/.novelgo/llm_config.enc` | AES-256，代码内 `appKey` 解密，无外部口令 |
| GitHub PAT | 根目录 `github_pat.txt` | `.gitignore` 忽略，**绝不入库** |
| SSH config | `C:\Users\Administrator\.ssh\config` | Host github.com → ssh.github.com:443 |
| CI secret | `DEEPSEEK_API_KEY`（Repository secrets） | 注入 `NOVELGO_EVAL_KEY` 给夜跑评估用 |
| MinGW/gcc | `C:/tools/mingw64/bin` | winlibs GCC 16.2.0 |
| sqlite3 headers | `C:/tools/sqlite3-headers` | 由 go-sqlite3 复制 |

---

## 9. 文档变更记录

| 日期 | 变更 |
|---|---|
| 2026-10-16 | 初版：固化技术状态、版本矩阵、待办优先级(P1–P3)、升级护栏、已否决方案、环境凭证 |
| 2026-10-16 | P1 完成：`release.yml`(3处)+`verify-macos-codesign.yml`(1处) Wails CLI 对齐 `v2.16.0`；同步更新 §2/§5/§9 |
| 2026-10-16 | 纠正 §5 误判：P2-1 人物关系图可视化**早已实现并入库**（F-5/F-6，`3fc049d`/`02faead`），此前「前端没有关系图 UI」为漏扫；同步 §3/§5/§9 |
| 2026-10-16 | P2-2 完成：**数据目录迁移做成真正可用功能**。新增 `internal/datamove` 包（含测试）、`platform.readConfiguredDataDir()` 让 `config.json:data_dir` 生效、`app.PickDirectory/UpdateDataDir` 重写为安全迁移+回退、前端 `GeneralConfigTab` 加「更改」按钮+进度条+非空确认框；wails 绑定重生成；全门禁绿 |
| 2026-10-16 | P2-3 技能内核完成（UI 待做）：新增内置编排技能 `signability-workflow`（manual/系统类），把 `story-blueprint`/`worldbuilding-design`/`signability-audit` 编排成六阶段「签约潜力工作流」引导式闭环。经 NovelGo 真实解析器验证可加载、`internal/skill` 包测试通过、Phase 4 QA 零缺陷（AC1–AC9 全满足）。内置技能总数 18→19。工作区 `skill-studio/20261016-135655-signflow/`（该编排技能后已退役删除，见 2026-10-26 行） |
| 2026-10-16 | P2-3 UI 入口完成：左侧 ActivityBar 新增「签约潜力工作流」面板（`PanelId="signability"` + `SignabilityWorkflowView`）。启动=合并式启用 `signability-workflow`(SetEnabledSkills 常驻注入)+经 `useChatLaunchStore` 通道发 kickoff 消息到 ChatPanel；含选书/配模型守卫与双语 i18n。前端门禁（prettier/i18n:check/tsc/build）全绿（历史机制：该编排技能后已退役删除，入口改由 pipeline 状态机驱动，见 2026-10-26 行） |
| 2026-10-16 | **三提取合一（Form B）落地**：风格/套路/心智 = 单一入口 + 一次触发 + 产物分立（非一份整合技能）。后端 `internal/pattern` 拆出共享前缀 `runPrefix`(边界→章节摘要→递归压缩)与 `runFinal`(按 flavor 分流 struct/mental)，新增 `ExtractPatternAll`(一次前缀多 flavor 产物)；`types.go` 加 `Flavors []string` + `ExtractPatternAllResult{struct,mental}`；`app/pattern_api.go` 加 `ExtractPatternAll` 方法(含 cancelMgr/mental retriever 同 `ExtractPattern`)。前端 `ExtractWorkspaceView` 改为单一页面(范围开关 style/struct/mental 默认全开)、`PatternExtractView` 改 `defaultFlavors` 多选、`PatternSessionView` 多产物 session(共享前缀一次产出，套路+心智卡片分流，mental 自动并入写作流)。wails 绑定 `App.js/App.d.ts/models.ts` 手工补 `ExtractPatternAll`(CI build 权威重生成)。i18n 双语补齐。全门禁绿(pretty/i18n:check/tsc/build/go build/go test)。已推送 master |
| 2026-10-16 | **三提取合一 UI 后续修复**：(1) 修复 `ExtractWorkspaceView` 因 `SectionHeader` 定义在组件体内部导致的 eslint "Cannot create components during render" error（2 errors），让 CI `lint-frontend` 恢复绿色；(2) 将叙事套路/心智、风格画像两个子面板改为独立圆角卡片容器（`rounded-xl border bg-card`），明确章节选择器只属于叙事卡片，消解用户「风格画像是否也读这 15 章」的歧义。全门禁绿（prettier/i18n:check/tsc/build/eslint 0 errors） |
| 2026-10-26 | 退役清理：删除已停用的内置编排技能 `signability-workflow.md.disabled`（2026-10-18 起「签约潜力工作流」入口由 pipeline 状态机驱动，技能零代码引用）；同步修正 §3 技能总数（19→26 现值）与 §3/§5 编排层描述 |
