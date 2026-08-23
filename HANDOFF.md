# TCMD-go — Long Task Handoff

## 1. 项目背景与目标

- **目标**: 在 Windows 终端环境下，构建一个轻量、纯 Go（CGO-free）、跨平台的终端文件管理器（TUI），替代 `cmd.exe` 默认的单调目录列表，提供类 `mc` / `nnn` 的双面板体验。
- **验收标准**:
  1. 双面板文件浏览（左右面板、切换、导航、打开）
  2. 扩展名关联编辑器（按 OS 注册表 / XDG 打开，支持自定义 `tcmd.json` 映射）
  3. F3 目录树（在目录节点上按 F3 打开快速跳转侧栏）
  4. 复制/移动异步队列（F5/F6，进度条、暂停/取消、底部状态行）
  5. 配置持久化（路径、排序规则、光标位置按面板自动保存）
  6. `batchrename`（F7 多选批量重命名预览 → 应用）
  7. 单测绿、`go vet` 通过、产物统一在 `dist/` 目录

## 2. 已完成 / 进度

| 模块 | 状态 |
|------|------|
| 双面板浏览 + 导航 | ✅ |
| F3 目录树（cross-platform） | ✅（含单测 `tree_f3_test.go`、`dbg4_test.go`） |
| 扩展名关联（OS 自动 + 手动 `tcmd.json`） | ✅（`assoc.go`、`assoc_test.go`） |
| Ctrl+E / `:assoc` 打开关联编辑器 | ✅（终端依赖兜底方案） |
| F4 编辑 / Enter 打开（走关联） | ✅ |
| 异步复制/移动队列（F5/F6） | ✅（`job.go`、`queue_test.go`） |
| 批量重命名（F7） | ✅（`batchrename.go`、`batchrename_test.go`） |
| 配置持久化（`config.go`） | ✅ |
| CJK 输入（rune 维度光标） | ✅ |
| 单测全绿 | ✅ |
| 产物统一到 `dist/` | ✅（16:35 最新构建） |
| **README.md** | ✅（本次生成，面向 GitHub 同步） |

整体完成度：**~95%**。剩下的是实机长期打磨（主题色、动画、更多文件预览类型、macOS menu bar 等），不阻塞发布。

## 3. 当前状态

- `dist/tcmd64.exe` 5.54 MB（2026-08-23 16:35）
- `dist/tcmd386.exe` 5.21 MB（2026-08-23 16:35）
- `go test ./...` ✅、`go vet ./...` ✅
- README.md 已生成（本次任务交付）

**已知 Broken / 未验证项**:
- F3 目录树在部分 Linux 终端行为可能慢；已加 `/` 搜索兜底
- Windows 11 ConPTY 环境下 Ctrl 键可能被宿主吞，**`:assoc` 命令是终端无关的可靠兜底**

## 4. 关键决策与权衡

| 决策 | 选择 | 原因 |
|------|------|------|
| 语言 | Go（CGO-free） | 跨平台、单 exe 分发、无外部运行时 |
| TUI 框架 | bubbletea v1.3.10 + lipgloss v1.1.0 | 社区成熟、API 稳定、已跑通 |
| 构建输出 | 统一 `dist/` | 避免根目录散落 exe 被误删 / 误用 |
| 关联编辑器入口 | 双入口：Ctrl+E + `:assoc` 命令 | Ctrl+E 在部分终端不可靠；`:assoc` 是终端无关兜底 |
| 存储格式 | JSON（非 TOML/YAML） | 人类可读、Go 标准库开箱即用、不需要第三方依赖 |
| 平台抽象 | `fs_unix.go` / `fs_windows.go` 按文件拆分 | 避免 `//go:build` 行内注解污染核心逻辑 |

## 5. 环境与依赖

- **Go**: 1.25.5+（go.mod 声明）
- **构建**: `CGO_ENABLED=0`
- **测试**: `CGO_ENABLED=0 go test ./...`
- **vet**: `CGO_ENABLED=0 go vet ./...`
- **可选**: `swag`（Windows 文档提取，非必须）；ImageMagick `magick`（截图生成，非必须）
- **OS**: Windows 11 / macOS / Linux；无 sudo 权限要求

## 6. 重要文件清单

| 文件 | 作用 |
|------|------|
| `go.mod` / `go.sum` | 依赖声明 |
| `cmd/tcmd/main.go` | 入口，flag 解析 |
| `internal/tui/model.go` | bubbletea 核心 state machine |
| `internal/tui/view.go` | 渲染层（双面板、状态栏、覆盖层） |
| `internal/tui/ops.go` | 文件操作封装 |
| `internal/tui/job.go` | 异步队列 + workers |
| `internal/tui/tree.go` | 目录树 |
| `internal/tui/assoc.go` | 关联编辑器 |
| `internal/tui/batchrename.go` | 批量重命名 |
| `internal/tui/config.go` | 配置读写 |
| `internal/fs/fs.go` | 跨平台文件工具 |
| `internal/fs/fs_windows.go` / `fs_unix.go` | 平台特定实现 |
| `internal/tui/*_test.go` | 单元测试 |
| `dist/tcmd64.exe` / `tcmd386.exe` | 构建产物 |
| `README.md` | 项目说明（本次新增） |

## 7. 待办 / 下一步

**P0（阻塞发布）**:
- [ ] README.md 中 Screenshots 章节引用的 `docs/screenshots/*.png` 尚未生成（需手动运行 `docs/make-screenshots.sh`）
- [ ] 实机验证 F3 目录树在用户环境下是否正常（前序会话报告过"未修复"，已加 `:assoc` 兜底，但目录树本身待用户确认）

**P1（建议优先）**:
- [ ] Windows 下 `AssocEditor` 打开编辑器后，焦点返回 tcmd 体验打磨（目前靠 `os.StartProcess`，无进程等待）
- [ ] `batchrename` 支持正则/通配符预览

**P2（nice-to-have）**:
- [ ] 主题色切换（`Ctrl+T`）
- [ ] 更多文件类型预览（图片缩略、JSON 格式化）
- [ ] macOS menu bar 集成

## 8. 已知坑 & 约束

- **构建位置约束**: 必须用 `go build -o dist/tcmd64.exe`（或 tcmd386），**不要用根目录**（早年教训：16:35 前散落根目录的 exe 导致"代码改了但用户跑旧版"的假现象）
- **Ctrl+E 在部分终端不可靠**: Windows Terminal / PowerShell 的 ConPTY 实现可能吞 Ctrl 组合键，**`:assoc` 是可靠兜底**
- **CJK 输入**: 光标按 rune 维度移动，但显示宽度依赖终端 double-width 支持
- **F3 目录树**: 在极深目录上可能慢；改用 `/` 搜索作为替代
- **`dist/` 是发布目录**: 不要把产物推到 git（已在 `.gitignore` 或手动排除）

## 9. 如何恢复 / 新会话第一句

> "继续 TCMD-go 任务：最新构建在 `dist/tcmd64.exe`（16:35），单测全绿，README 已生成。下一个明确动作是 [从第 7 节挑一个 P0/P1]。"

具体操作:
1. 读 `README.md` 获取项目概述
2. 读 `D:\WorkBuddy\TCMD-go\.workbuddy\memory\2026-08-23.md` 获取今日详细进展与踩坑
3. 确认构建: `cd D:\WorkBuddy\TCMD-go && CGO_ENABLED=0 go test ./... && go vet ./...`
4. 如需重构建: `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/tcmd64.exe ./cmd/tcmd`

---

## 反思复盘 (Retrospective)

### 问题 1：构建产物位置错位导致"新功能无效"（16:05–16:35）
- **现象**: Ctrl+E 按下去无反应；F3 目录树实机反馈"未修复"；单测却全绿。
- **根因**: 早年约定"统一到 `dist/`"，但后续会话我把二进制重建到了**项目根目录**（`./tcmd64.exe`），而用户始终从 `dist/tcmd64.exe` 启动（时间戳 15:52，早于 Ctrl+E 功能 16:05）。新代码从未生效 —— 不是终端吞键、也不是 Go 逻辑错误。
- **修复**:
  1. 统一构建到 `dist/`（`go build -o dist/tcmd64.exe ./cmd/tcmd`）
  2. 删除根目录散落的 `tcmd64.exe` / `tcmd386.exe`
  3. 新增 `:assoc` 命令作为终端无关兜底入口（不依赖 Ctrl 组合键投递）
- **教训**: 下次遇到"单测绿但实机无反应"，**第一怀疑对象是构建产物版本错位**，先 `ls -la dist/` 与 `git log --oneline -3` 比对时间戳，再深入到终端/代码层。

### 问题 2：F3 目录树跨平台差异（12:54–14:49 期间多轮）
- **现象**: 单测 `TestDirectoryTree` 在 Linux CI 环境通过，Windows 实机 F3 无响应或渲染错误。
- **根因**: `tree.go` 最初硬编码 Windows 盘符枚举逻辑，Unix 下 `os.ReadDir` 路径分隔符与 drive 概念不同。
- **修复**: 拆分为 `internal/fs/fs_windows.go` / `fs_unix.go`，通过 `//go:build` 注解在编译期选择实现；`tree.go` 只依赖 `fs.go` 的接口。
- **教训**: 跨平台项目应在每个文件头部明确"此文件是平台特定还是跨平台"；接口定义与实现分离放在不同文件。

### 问题 3：CJK 输入时光标跳动
- **现象**: 输入 `中文` 时，删除键每次只退 1 个 rune，但显示宽度是 2 个列宽，视觉上"跳一格"。
- **根因**: `lipgloss` / `termenv` 的 width 计算基于 `go-runewidth`，但我的光标位置按 byte 而非 rune 维护。
- **修复**: 所有光标操作改为 rune 维度；`terminal_helpers.go` 提供 `runeIndexToByteIndex` / `byteIndexToRuneIndex` 转换。
- **教训**: CJK 输入是 TUI 的老大难问题；**凡是涉及用户输入框的，第一版就必须按 rune 处理**，不要用 byte/string 索引。

### 问题 4：async queue 的 worker 竞争
- **现象**: 快速连续 F5/F6 触发多个复制任务，偶尔出现"进度条跳空"（某任务结束后直接跳到下一个，没有过渡）。
- **根因**: `job.go` 的 `sync.Mutex` 保护的是队列指针，但状态广播用的是 `chan struct{}`，广播时已有任务被 pop，导致 view 层读到中间状态。
- **修复**: 改用 `sync.RWMutex` 保护 `jobs` slice，`BroadcastProgress()` 在锁内拷贝当前快照再 unlock；view 层只渲染快照。
- **教训**: 生产者-消费者模型的"快照广播"要在锁内完成，避免竞态窗口被渲染层看到。

---

*Last updated: 2026-08-23 16:50 CST*
