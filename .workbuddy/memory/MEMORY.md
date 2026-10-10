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
- 沙箱无 gcc → sqlite-vec/onnxruntime 两个 CGO 包无法编译，完整构建需开发机。
- 仓库 autocrlf=true 检出为 CRLF，`gofmt -l` 会全量误报，勿据此改格式。
