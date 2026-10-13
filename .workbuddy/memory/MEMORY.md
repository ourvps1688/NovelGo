# NovelGo 项目长期备忘

## 项目性质
- NovelGo = sigpanic/goink（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0 + NOTICE 附加条款）的合规派生 fork。
- 硬性合规：必须保留 "Based on Goink by 响滩 (sigpanic) — https://github.com/sigpanic/goink" 与 "This is a modified version of Goink..." 声明（README/NOTICE/About 对话框），不得移除或暗示原作者背书。

## 关键约定
- 模块路径 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`；小说元数据文件 `novelgo.md`。
- 品牌 mark：N+G 字母组合 monogram（用户指定，明确不要书的意象）；wails 生成类型名前缀 `novelgo_novelgo`。
- skills 仓库 `ourvps1688/NovelGo-skills`、主仓库 `ourvps1688/NovelGo`（已创建并推送，master/main 分支）。
- git 历史已 unshallow；`origin`=ourvps1688/NovelGo，`upstream`=sigpanic/goink。

## UI 方向
- 用户认为当前前端"丑"，但**现阶段只做纯皮肤换肤**：仅改 `frontend/src/index.css` 颜色 token，不动组件/布局/功能入口。
- 审美参考 AI-NWA（ExplosiveCoderflome/AI-Novel-Writing-Assistant）。**已按用户提供的 AI-NWA 真实截图对版**：极简黑白灰"墨色"风（Notion/Vercel 挂）——亮色 primary 近黑 `oklch(0.24 0.005 260)`、暗色反转近白；白卡片 + 浅灰页底 + 中性细边框；**不是**彩色强调。语义色（tag/tool/danger/success）保留原样。
- 沙箱 Bash 外网被拦；AI-NWA 视觉以用户贴图为准（微信截图_20260813220038.png）。

## 环境注意
- **前端提交前的 CI 硬闸门（本地必跑）**：`npx prettier --check "src/**/*.{ts,tsx}"` 与 `npm run i18n:check`（后者会扫描源文件硬编码中文，中文文案必须进 i18n locales）。仅跑 tsc/vite/vitest 不够——Checks #41 曾因此报红。eslint CI 不限 warnings 数（0 error 即过）。
- **沙箱已装 MinGW/gcc**：winlibs GCC 16.2.0 (UCRT,x86_64) 在 `C:/tools/mingw64/bin`；`go` 在 `C:/Program Files/Go/bin`、`wails` 在 `C:/Users/Administrator/go/bin`（均已在 PATH）。wails dev 已跑通（见每日日志）。
- **Windows 构建前置**：CGO 编译需 `sqlite3.h` → 设 `CGO_CFLAGS=-IC:/tools/sqlite3-headers`（该目录由 mattn/go-sqlite3@v1.14.44 的 `sqlite3-binding.h` 复制而来，详 cross-platform-build.md §7）。构建命令须显式 `export PATH="/c/tools/mingw64/bin:$PATH"`（新 shell 不继承 User PATH）。
- 本地 ONNX 向量检索为可选能力，缺 `onnxruntime.dll`+`build/bin/models/vocab.txt` 时仅该功能不可用，不影响主程序。
- 仓库 autocrlf=true 检出为 CRLF，`gofmt -l` 会全量误报，勿据此改格式。
- **前端改动需重建 dist 才在 webview 生效**：`wails dev` 启动时跑 `frontend:build`（npm run build → tsc -b && vite build）生成 `frontend/dist`，webview 加载的是 dist 构建产物（带 hash 的固定文件名，如 `index-*.js`），**不是** Vite 实时源码；后台 `npm run dev` 仅服务浏览器预览。故改前端源码后，必须 `npm run build` 并重启 `wails dev`（旧的 webview 不会热更到新 dist，表现为"界面没刷新/没加载"）。后台 vite 实时 HMR 只影响独立浏览器预览，不影响 wails 桌面窗口。
- **wails 版本自动对齐**：本机 wails CLI = v2.16.0（`C:/Users/Administrator/go/bin/wails`，Sep 19 安装）。`wails dev` 启动会把 go.mod 的 wails 版本自动改写为 CLI 版本。仓库 baseline 是 v2.12.0，但工作区会不断被 dev 改回 v2.16.0——回退 2.12 无意义。**结论：正式把项目锁定到 v2.16.0**（go mod tidy 后提交 go.mod/go.sum），该升级对业务代码零影响（仅用 pkg/runtime 基础 API，v2.12→v2.16 签名未变，go build 通过）。
