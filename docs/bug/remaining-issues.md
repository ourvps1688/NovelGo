# 待解决问题清单

整理日期: 2026-07-04
来源: dev 分支代码审查

## 架构级重构（需较大改动）

### A1. style_sample 迁移到全局存储

**现状**: 素材存储在 `novels/{id}/style_samples/` 下，跟小说绑定
**目标**: 迁移到 `~/.NovelGo/style_samples/`，全局共享
**方案**:
- 参照 `internal/skill/store.go` 的 Store 模式，改为结构体：
  ```go
  type Store struct {
      mu     sync.RWMutex
      logger *slog.Logger
      dir    string  // ~/.goink/style_samples/
  }
  ```
- IO 方式从 `git.ReadFile/WriteFile` 改为 `os.ReadFile/os.WriteFile`（无 git 版本管理）
- 需要数据迁移逻辑（从旧路径搬到新路径）

**关联问题**: 当前 style_sample 是纯函数无结构体，无法持有 logger，迁移时一并解决

### A2. git.Repo 加 logger 字段

**现状**: `Repo` 结构体只有 `dir` 和 `gitBin`，无 logger
**问题**: 9 处 `git.New()` 调用中，部分传空 gitName/gitEmail（如 style_sample 的 `git.New(novelID, "", "")`），且无法做结构化日志
**方案**:
- `Repo` 加 `logger *slog.Logger` 字段
- `New()` 签名加 logger 参数
- 更新 9 处调用方：
  - `app/` 层：用 `a.logger` 传入
  - `internal/style_sample/store.go`：需要 A1 完成后用 `s.logger` 传入
  - style_sample 迁移后不再需要 git.New，此调用可能消除

### A3. git.CommitDiff 区分正常/异常错误

**现状**: `git show` 失败统一忽略错误，content 设为空字符串
**问题**: 用户无法区分"文件确实为空"和"读取失败"
**方案**:
- `git show hash:file` 失败 → 判断错误消息是否包含 "does not exist" → 文件不存在是正常的，设空 content
- 其他错误（git 对象损坏等）→ return error
- 依赖 A2 的 logger 注入，用 `r.logger.Debug` 记录正常跳过

### A4. import API 层做了太多事情

**现状**: `app/import_novel.go` 的 `ImportNovel` 函数包含了文件解析、DB 事务、git commit 等全部逻辑
**问题**: 按照项目其他模块的分层设计（API 层只做参数组装和调用，内部包做业务逻辑），import 的业务逻辑应该在 `internal/import/` 包中
**方案**:
- `internal/import/` 包提供 `Import(ctx, logger, ...)` 高层函数，封装完整的导入流程
- `app/import_novel.go` 只做 Wails 绑定和参数转换

### A5. epub 跳过文件需要用户反馈

**现状**: epub 解析跳过某些章节文件时只记 `slog.Warn`，用户不知道少了章节
**方案**:
- `Book` 结构加 `SkippedFiles []SkippedFile` 字段
- 前端导入完成后显示"导入了 N 章，跳过了 M 章"

## 配置/CI 修复

### C1. CI Windows CGO 测试跳过

**文件**: `.github/workflows/test.yml`
**问题**: 未设置 `CGO_ENABLED=1`，Windows runner 可能跳过 CGO 代码测试
**方案**: 在 test job 中添加 `CGO_ENABLED: 1` 环境变量

### C2. Makefile clean 不兼容 Windows

**文件**: `Makefile` L66-67
**问题**: 使用 `rm -rf`，原生 Windows 不可用
**方案**: 加平台判断，Windows 用 `Remove-Item -Recurse -Force`

## 待确认/暂不处理

| 编号 | 问题 | 说明 |
|------|------|------|
| L2 | UnixNano ID 无唯一性 | 桌面单用户够用，确认不改 |
| L6 | List 静默跳过失败文件 | 优先级低 |
| M2 | epub 路径穿越检查 | 已撤销，zip 是天然安全边界 |
| M12 | WorkspaceView 错误 UX | 保留 console.error，后续统一规划通知系统 |
| H6 | CI CGO 跳过 | 列在 C1 |
| L12 | Makefile Windows | 列在 C2 |

## 已完成的修复

详见 `docs/bug/dev-code-review-2026-07-04.md` 和 git log:
- `94fd5a0` — C1, H1+M1, H2, H3, H5, M2, M3, M4, M7+M8, M9, M10
- `5e32a1c` — M11, M12, M13, M14, M15, M16, M17
- `86a2224` — L1, L3, L4, L5, L7, L8, L9, L10, M2-revised
- `e06ca62` — L4(logger inject), L8(select+ctx), L11(comment)
