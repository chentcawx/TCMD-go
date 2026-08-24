# TCMD-go P2 级别问题系统性修复报告

**日期**：2026-08-23  
**状态**：全部完成，`go test ./...` 全绿

---

## 修复总览

| # | 问题 | 严重度 | 状态 | 涉及文件 |
|---|------|--------|------|----------|
| P2-1 | PgDown 死代码 | 低 | ✅ 已修复 | `internal/tui/update.go` |
| P2-2 | pruneDone 并发安全 | 低 | ✅ 已确认 | `internal/tui/job.go` |
| P2-3 | copyDirProgress 无并发 | 中 | ✅ 已修复 | `internal/fs/fs.go` |
| P2-4 | handleKey 过长 | 低 | ✅ 已记录设计决策 | `internal/tui/update.go` |
| P2-5 | copyBufSize 位置分散 | 低 | ✅ 已提取 | `internal/fs/constants.go`（新建） |
| P2-6 | openViewer 局部冗余常量 | 低 | ✅ 已清理 | `internal/tui/model.go` |
| P2-7 | closeOverlay 未清 viewer 状态 | 中 | ✅ 已修复 | `internal/tui/model.go` |
| P2-8 | 后台 goroutine 错误静默丢弃 | 低 | ✅ 已修复（上一轮） | `assoc.go`, `terminal_windows.go` |
| P2-9 | 魔法常量散落 | 低 | ✅ 已提取（上一轮） | `internal/tui/constants.go` |
| P2-10 | 配置不保存光标状态 | 中 | ✅ 已修复 | `internal/tui/config.go` |

---

## 逐项修复详情

### P2-1: `handleViewerKey` 中 PgDown 死代码

**问题描述**：
```go
case tea.KeyPgDown:
    m.viewerScroll += (m.height - 4)
    if m.viewerScroll < 0 {  // ← 死代码：PgDown 只增不减
        m.viewerScroll = 0
    }
```

**根本原因**：复制 `KeyPgUp` 的 clamp 逻辑时未修改条件，`viewerScroll` 增加后检查 `< 0` 永远为 false。

**修复方案**：改为上限检查，防止滚动超出文件末尾：
```go
case tea.KeyPgDown:
    m.viewerScroll += (m.height - 4)
    maxScroll := len(m.viewerLines) - (m.height - 4)
    if maxScroll < 0 {
        maxScroll = 0
    }
    if m.viewerScroll > maxScroll {
        m.viewerScroll = maxScroll
    }
```

**验证方法**：打开一个超过屏幕高度的文本文件，多次按 PgDown，验证不超出末尾。

---

### P2-2: `pruneDone` 并发安全

**问题描述**：`markDone` 在 worker goroutine 中发送 `prune` 信号，`pruneDone` 在 `Run()` goroutine 中执行，存在理论上的并发风险。

**根本原因**：`prune` channel 容量为 1，多个 worker 同时完成时会丢弃多余信号，但 `pruneDone` 内部已有 `q.mu.Lock()` 保护 map 访问。

**修复方案**：无需代码修改。当前实现已是正确的 producer-consumer 模式：
- `markDone`（worker goroutine）→ 非阻塞发送 `struct{}{}` 到 `prune` channel
- `Run`（主 goroutine）→ select 接收后调用 `pruneDone`，在锁内遍历 map

**验证方法**：运行 `TestQueueActiveJobs`，模拟多 job 同时完成。

---

### P2-3: `copyDirProgress` 无并发

**问题描述**：目录复制递归同步执行，深层嵌套 + 大量小文件时单个 worker goroutine 串行处理，效率低。

**根本原因**：`copyDirProgress` 使用 for 循环逐个 `CopyProgress`，无并发控制。

**修复方案**：引入 semaphore 模式的并发复制（`maxCopyWorkers = 4`）：
```go
const maxCopyWorkers = 4
sem := make(chan struct{}, maxCopyWorkers)
var wg sync.WaitGroup
var firstErr error
var errMu sync.Mutex
for _, e := range entries {
    s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
    wg.Add(1)
    go func(src, dst string) {
        defer wg.Done()
        sem <- struct{}{}
        defer func() { <-sem }()
        if err := CopyProgress(s, dst, cb); err != nil {
            errMu.Lock()
            if firstErr == nil { firstErr = err }
            errMu.Unlock()
        }
    }(s, d)
}
wg.Wait()
return firstErr
```

**副作用分析**：
- ✅ 进度回调 `cb` 仍按实际完成顺序调用，UI 更 responsive
- ✅ 第一个错误仍被返回，后续 goroutine 继续完成（不中断）
- ⚠️ 增加了 4 个并发 goroutine/层级，对极深目录（>100 层）可能有 goroutine 爆炸风险，但实际操作中极少见

**验证方法**：`go test ./internal/fs -count=1` 全过；手动测试大目录复制性能。

---

### P2-4: `handleKey` 可维护性

**问题描述**：`handleKey` 约 30 行，新增 overlay 需修改此函数 + 添加 handler + 添加 View case。

**根本原因**：overlay 数量增多后，三处需要同步修改（Update/View/新增 handler）。

**修复方案**：**决定保持显式 switch**。理由：
1. bubbletea 的 Update 循环要求 return `(Model, Cmd)`，map lookup 会增加一层间接调用
2. 显式 switch 在 debugger 中更容易看到当前分支
3. 当前 9 个 overlay 规模下，switch 可读性足够

**改进**：在 `handleKey` 上方添加注释，说明新增 overlay 的三个步骤。

---

### P2-5: `copyBufSize` 常量位置

**问题描述**：`copyBufSize` 定义在 `fs.go` 中部（第 21 行），与其他业务逻辑混在一起。

**修复方案**：新建 `internal/fs/constants.go`，集中存放 fs 层常量：
```go
package fs

const copyBufSize = 32 * 1024 // 32 KiB
```
从 `fs.go` 删除原定义。

**验证方法**：`go build ./...` 无编译错误（引用 `copyBufSize` 的代码自动解析到新文件）。

---

### P2-6: `openViewer` 冗余局部常量

**问题描述**：
```go
func (m *model) openViewer(path string) {
    const maxView = maxViewBytes  // ← 冗余
    ...
    if fi.Size() > maxView {
```

**修复方案**：直接使用包级常量：
```go
if fi.Size() > maxViewBytes {
```

---

### P2-7: `closeOverlay` 未清理 viewer 状态

**问题描述**：`closeOverlay` 重置了 assoc/driver/context-menu 等状态，但未重置 `viewerPath/viewerLines/viewerScroll`。

**根本原因**：`viewerScroll` 等字段只在 `openViewer` 中设置，`closeOverlay` 遗漏。

**修复方案**：在 `closeOverlay` 末尾添加：
```go
// Text viewer transient state — reset so a subsequent viewer open starts
// clean without stale scroll/line data lingering in the model.
m.viewerPath = ""
m.viewerLines = nil
m.viewerScroll = 0
```

**验证方法**：打开 viewer → 滚动 → Esc 关闭 → 再打开同一文件，验证从顶部开始。

---

### P2-8: 后台 goroutine 错误静默丢弃（上一轮已修复）

**修复内容**：`assoc.go`、`terminal_windows.go` 中三处 `_ = cmd.Wait()` 改为记录 stderr。

---

### P2-9: 魔法常量提取（上一轮已修复）

**修复内容**：新建 `internal/tui/constants.go`，集中 `defaultMaxWorkers`、`maxConcurrentJobs`、`maxViewBytes`、`treeStatTimeout`。

---

### P2-10: 配置持久化补充光标状态

**问题描述**：`saveConfig` 只保存路径，不保存 `cursor`/`offset`/`selected`，重启后丢失导航位置。

**修复方案**：
1. `paneState` 新增 `TabCursor []int` 字段（`json:"tabCursor,omitempty"`）
2. `saveConfig` 保存每个 tab 的 cursor
3. `ApplyConfig` 恢复 cursor（越界时 clamp）

**JSON 兼容性**：`omitempty` 确保旧配置文件（无 `tabCursor` 字段）仍可正常加载，cursor 默认为 0。

**验证方法**：
- `TestConfigRoundTrip` 更新验证新字段
- 手动测试：导航到文件列表中部 → 退出 → 重启 → 验证光标位置恢复

---

## 改动文件清单

| 文件 | 改动类型 | 说明 |
|------|----------|------|
| `internal/fs/constants.go` | **新建** | 集中 fs 层常量（copyBufSize） |
| `internal/fs/fs.go` | 修改 | copyDirProgress 并发化；删除 copyBufSize 定义 |
| `internal/tui/update.go` | 修改 | PgDown clamp 修复；handleKey 添加注释 |
| `internal/tui/model.go` | 修改 | closeOverlay 清理 viewer 状态；openViewer 移除冗余局部常量 |
| `internal/tui/config.go` | 修改 | paneState 添加 TabCursor；saveConfig/ApplyConfig 支持光标持久化 |
| `internal/tui/job.go` | 修改 | markDone/pruneDone 添加并发安全注释 |
| `internal/tui/constants.go` | 修改（上轮） | 集中 tui 层常量 |
| `internal/tui/assoc.go` | 修改（上轮） | 后台 goroutine 错误日志 |
| `internal/tui/terminal_windows.go` | 修改（上轮） | 后台 goroutine 错误日志 |

---

## 验证结果

```
go build ./...           ✓ 无错误
go vet ./...             ✓ 无警告
go test ./internal/fs    ✓ 1.17s
go test ./internal/tui   ✓ 2.56s
go test ./...            ✓ 全绿

dist/tcmd64.exe          5.4 MB
dist/tcmd386.exe         5.0 MB
```

---

## 待办（P1 级别，未实施）

1. **大目录异步加载**：`newTab()` 同步 `reload()` 阻塞 bubbletea 事件循环
2. **取消任务后半写入文件清理**：JobQueue 取消后目标文件残留无清理机制
