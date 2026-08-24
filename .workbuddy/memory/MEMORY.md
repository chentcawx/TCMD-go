# TCMD-go 项目记忆

## 项目定位
- **名称**：TCMD = TUI Command。纯 Go 实现的 Total Commander 风格**双窗口多标签**文件管理器。
- **目标平台**：Windows 32 位（GOARCH=386）与 64 位（GOARCH=amd64）。绿色免安装 exe，无外部依赖。
- **硬约束**：避免使用任何 web 控件（不用 WebView2 / Wails / Electron），节约系统资源；纯原生终端 TUI 渲染。

## 架构决策（2026-08-23 经用户确认）
- **渲染后端**：`bubbletea`（终端 TUI）。理由：最贴合“TUI Command”命名、最省资源、天然零 web、纯 Go 32/64 位编译无 CGO。
- **备选评估**：
  - Walk（原生 Win32 GUI）—— 最像 TC 桌面体验但仅 Windows；
  - Gio（跨平台即时模式 GUI）—— 依赖 OpenGL，远程桌面/虚拟机易白屏（mediadown-go 已踩坑）。
  - 两者均未被选。
- **模块结构**：
  - `internal/fs`：跨平台文件操作（列举/复制/移动/删除/新建/隐藏属性）。Windows 隐藏属性用 `golang.org/x/sys/windows` 的 `GetFileAttributes` + `UTF16PtrFromString`。
  - `internal/tui`：bubbletea model/view/update/ops。`model.go`（状态机）/ `update.go`（键位+操作触发）/ `view.go`（渲染）/ `ops.go`（文件操作纯函数）。
  - `cmd/tcmd/main.go`：入口，`tea.NewProgram(m, tea.WithAltScreen())`。

## 键位（TC 风格）
Tab 切面板 · ↑↓/jk 移动 · Enter 进入 · Backspace 上级 · Space/Insert 选择 · Ctrl+A 全选 · Ctrl+R 刷新 · **Alt+←/→ 切标签** · Ctrl+T 新标签 · Ctrl+W 关标签 · F3 查看 · F4 编辑/关联打开 · F5 复制 · F6 移动 · F7 新建 · F8/Del 删除 · `:` 命令行 · `?` 帮助 · Esc 取消 / Q 退出 · **Ctrl+E 打开关联编辑器** · **:assoc 命令打开关联编辑器（终端无关兜底）**。
**新增强健（v0.5.0 起）：** 字母键快速定位（case-insensitive 前缀匹配）· **Alt+1/2/3** 排序列（名/日期/大小）· **Alt+R** 反转排序方向 · 鼠标点击列表首行排序列头切换排序，同列二次点击反转；双击路径栏段跳转至该级目录。· 鼠标滚轮逐行滚动（不再半屏跳跃）。

## 关键约定（2026-08-23 固化）
- **构建产物统一在 `dist/`**：`go build -o dist/tcmd64.exe ./cmd/tcmd`（不要建到根目录）。
- **Ctrl+E 在部分终端不可靠**（Windows Terminal / ConPTY 吞键）：`case tea.KeyCtrlE` 的 `handleNormalKey` 分支是终端相关入口；`case "assoc"` 的 `beginCommand` 分支是终端无关兜底。
- **跨平台文件操作**：`internal/fs/fs.go` 是跨平台接口；平台特定实现在 `fs_windows.go` / `fs_unix.go` 按 `//go:build` 文件级拆分（不要在 `.go` 文件内用行级 build tag 混合平台代码）。
- **异步队列广播**：快照在锁内拷贝再 unlock，避免 view 看到中间态。
- **CJK 输入**：光标按 rune 维度移动（`terminal_helpers.go`），不要用 byte/string 索引。

## MVP 范围（首版已实现）
双窗口 + 每面板多标签 + 文件列表（目录优先排序）+ 键盘导航/选择 + 复制/移动/删除/新建目录/查看（内置只读 viewer）+ 命令行（目录跳转或 shell 执行）+ 确认/输入对话框。

## 后续迭代清单（未实现，按优先级）
1. FTP / 网络邻居
2. 压缩包浏览（zip/7z/rar）
3. 同步目录、比较文件
4. 批量重命名（正则/计数器）
5. 内置编辑器（F4，当前仅提示后续）
6. 快速搜索（quick search）/ 增量筛选
7. ~~文件关联打开、自定义列、排序规则切换~~（v0.5.0 已实现 Ctrl+1/2/3 + Alt+R + 鼠标列头）
8. 配置持久化（ini/json：列宽、默认目录、键位）—— Tab 排序状态已持久化
9. 中文 IME 输入（当前命令行/输入框仅 ASCII，CJK 需 IME 支持）
10. 后台任务 + 进度条（当前复制/删除同步执行，大文件会短暂卡 UI）
11. 目录大小递归计算（状态栏当前仅统计文件）

## 构建 / 验证
- 64 位：`go build -o dist/tcmd64.exe ./cmd/tcmd`
- 32 位：`GOARCH=386 go build -o dist/tcmd386.exe ./cmd/tcmd`
- 测试：`go test ./internal/fs`
- 绿色分发：直接拷贝 exe 即可，无 DLL/运行时依赖（纯 Go）。
## 测试矩阵
- 宽度：`TestAmbiguousWidthAlignsColumns`（em dash 文件名行宽 ≤ pane 宽）、`TestTruncateDWUsesRunewidthWidth`（truncateDW 保守尺子）
- 路径段：`TestSplitPathSegments`、`TestPathPrefixAtSegment`（Windows 盘符 + Unix 绝对路径边界）
- 进程测试：`TestRunWithFileStartsProcess`、`TestAltF7OpensCmdTerminal`、`TestJunctionIsDir` — 均有 `testing.Short()` 守卫，`go test -short ./...` 时 SKIP 避免弹窗
- 全量：`go test ./...` 全绿后提交
- **日常开发推荐**：`go test -short ./...` 静默运行不弹窗口；`go test ./...` 完整验证含进程行为

## 已知坑 / 约束
- bubbletea v1.3.10 **无** `KeyCtrlTab` / `KeyQ` 常量（标签切换改用 Alt+←/→；viewer 退出用 `msg.String()=="q"`）。
- `windows.GetFileAttributes` 必须传 `*uint16`（UTF-16），不能直接传 Go string。
- 终端 TUI 下中文目录名输入受 IME 限制（MVP 命令行仅 ASCII 路径）。
- **ANSI 宽度计数：必须用 `lipgloss.Width(s)`，**绝不能用 `runewidth.StringWidth(s)`**——后者把 ESC/`[`/`;`/digits 都按可见字符算，会误判 truncate 触发条件并偷吃掉 trailing reset，后续行继承打开的 SGR 样式（典型现象：左侧高亮 tab 截断后整个右 panel 跟着染色）。`view.go` 的 `truncateDW` 已经改成 ANSI-aware：CSI 整块保留 + `hasOpenSGR` 检测 + 自动闭合 `\x1b[0m`。
- **View() 末端防御：clampRowWidth(view, m.width)** 用 `lipgloss.Width` 兜底每行可见宽度 ≤ m.width，防止 ConPTY 在非最大化 Windows Terminal 下报的 width 与终端真实 drawable 宽度有微小偏差时整行超出触发 wrap 出现"右标签覆盖左标签"的伪串行效果。
- **测试矩阵**：用 `go test -v -run 'TestTruncateDW|TestClampRowWidth|TestViewRowsStayWithinWidth' ./internal/tui/` 覆盖截断 + clamp + 完整 View 路径。
- **bubbletea ctrl+key 行为**：Windows console 下 ctrl+a/b/c → `KeyCtrlA/B/C`（`\x01/\x02/\x03`），但 ctrl+1/2/3 本身不产生命名 KeyCtrl*，实际以 `KeyRunes{'1'/'2'/'3'}`（ASCII digit 49/50/51）或 `KeyRunes{0x11/0x12/0x13}`（控制字节）到达，取决于终端类型（Windows Terminal vs conhost）。排序热键改用 **Alt+1/2/3**（`msg.Type == tea.KeyRunes && msg.Alt && len(msg.Runes) == 1`）；`alt+r` 反转排序。`alt+字母` 同样以 `KeyRunes{rune, Alt:true}` 到达，用 `msg.Type == tea.KeyRunes && msg.Alt && len(msg.Runes) == 1` 判断。
- **pathPrefixAtSegment 根路径处理**：Windows 根路径如 `"E:\"` 的 segIdx=0 应返回 `"E:\\"`（保留尾部反斜杠），否则 `newTabAt("E:", "\\")` 打开无效路径；`filepath.Base("E:\\")` 返回 `"\\"`，需注意此边界。
- **配置路径规范化**：`ApplyConfig` 在加载 tab path 前必须调用 `normalizeTabPath`，将 `"E:."` / `"E:"` / `"E:/"` 等历史残次格式统一修正为 `"E:\\"`，避免 `filepath.IsAbs("E:.") == false` 导致误回退 homeDir()。
- **冒号盘符 JSON key 非法**：C: / D: 含冒号，不能直接作为 JSON object key；TabSortField 改用数组索引 `TabSortField[X]` 持久化（X 为面板槽位 0/1）。
- **鼠标滚轮跳半屏**：wheel 应调 `incrementRow(±1)` 而非 `pageMove(±1)`，后者每次跳约半屏（(height-6)/2 行）。
- **update.go 导入别名**：必须用 `tea "github.com/charmbracelet/bubbletea"`，否则 `tea.KeyRunes` 等引用报错。
- **`formatTime` 零值占位符必须与 `timeFieldW` 一致**：之前返回 16 空格（旧格式宽），导致 padLeftDW 输入宽于 timeFieldW=11，行宽溢出；改为 11 空格后对齐。
- **width 预算 off-by-one 根因**：formatEntry 的 rightSideW 公式必须包含所有分隔空格（mark+sp+size+sp+time=27，不是 26 或 28），否则 short-name 行溢出 1 格。
- **tabAt 坐标系统**：`tabAt(x, w)` 接收全局 X 坐标但内部按 pane 本地坐标累加（从 pos=0 开始）。调用方必须预先转换：左面板直接用 x，右面板需减去 `sepCol() + sepWidth`。`handleLeftClick` 的 tab bar 分支已做此转换（mouse.go:155-165）。
- **F3 目录树渲染 bug**：`renderNode` 必须将 `prefix` 拼入输出行（`line := prefix + connector + ...`），否则子目录缩进全部丢失——v0.5.7 已修复，`TestF3TreeIndentation` 覆盖。
- **drive picker 鼠标坐标**：`handleDrivePickerMouse` 中 `clickedIdx = Y - 10`（centerBox 居中后 drive 条目实际在 Y=10,11,12）。测试用 `[]struct{y,idx int}` 避免 range 索引/值混淆。
- **F3 目录树滚动**：`model.treeScroll` 记录垂直偏移；`renderTreeView` 按 visibleLines=h-4 截取可见子集；cursor 节点加 ▶ 标记；PageUp/PageDown/Home/End 支持自动滚动。
- **文件面板滚动条**：`renderVScrollbar()` 已实现但未接入 `renderList()`（测试行宽约束，后续需调整测试或改用 overlay 渲染）。
