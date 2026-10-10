# NovelGo 项目长期备忘

## 项目性质
- NovelGo = sigpanic/goink（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0 + NOTICE 附加条款）的合规派生 fork。
- 硬性合规：必须保留 "Based on Goink by 响滩 (sigpanic) — https://github.com/sigpanic/goink" 与 "This is a modified version of Goink..." 声明（README/NOTICE/About 对话框），不得移除或暗示原作者背书。

## 关键约定
- 模块路径 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`；小说元数据文件 `novelgo.md`。
- 品牌 mark：N+G 字母组合 monogram（用户指定，明确不要书的意象）；wails 生成类型名前缀 `novelgo_novelgo`。
- skills 仓库 `novelgo/novelgo-skills`、主仓库 `novelgo/novelgo`（均待用户在 GitHub 创建）。
- git 历史为 shallow（depth 1），upstream remote 即 origin=sigpanic/goink。

## 环境注意
- 沙箱无 gcc → sqlite-vec/onnxruntime 两个 CGO 包无法编译，完整构建需开发机。
- 仓库 autocrlf=true 检出为 CRLF，`gofmt -l` 会全量误报，勿据此改格式。
