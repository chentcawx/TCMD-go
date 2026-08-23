# tcmd

一个 32/64 位、零 CGo 依赖、纯 Go 实现的跨平台终端文件管理器（TUI）。

- **Go 1.25.5+**，零 CGo，可在 Windows / Linux / macOS 原生编译
- 二进制约 5 MB，无运行时依赖
- 基于 [bubbletea](https://github.com/charmbracelet/bubbletea) + [lipgloss](https://github.com/charmbracelet/lipgloss) 构建

---

## 目录

- [项目由来](#项目由来)
- [功能特性](#功能特性)
- [快速开始](#快速开始)
- [编译构建](#编译构建)
- [使用说明](#使用说明)
- [配置说明](#配置说明)
- [扩展关联规则](#扩展关联规则)
- [架构设计](#架构设计)
- [注意事项与已知问题](#注意事项与已知问题)
- [许可证](#许可证)

---

## 项目由来

在 Windows 上，`cmd.exe` / `powershell.exe` 仍是默认终端，但浏览文件体验极差。
`tcmd` 填补了这个空白：一个**快速、键盘驱动的文件管理器**，只要有终端就能运行，具备：

- 双面板布局（继承自 `cmd` / `mc` / `nnn` 的设计思路）
- 可扩展的文件类型关联（用系统已注册的默认程序打开）
- 异步复制 / 移动队列，支持暂停 / 取消与进度显示
- 批量重命名（写盘前可预览）
- 会话配置持久化（目录、排序规则、光标位置自动保存）

---

## 功能特性

| 功能 | 键位 | 说明 |
|------|------|------|
| 双面板浏览 | `←` `→` | 左右面板切换 |
| 导航 | `↑` `↓` `Enter` `Backspace` | 进入目录、返回上级 |
| 批量重命名 | `F2`（选中文件后） | 多选、预览、再写盘 |
| 用关联程序打开文件 | `F3` / `F4` / `Enter` | 优先查系统扩展名关联，否则回退内置预览 |
| 打开关联编辑器 | `Ctrl+E` / `:assoc` | 在任意输入框输入 `:` → `assoc` → 回车，打开扩展名-程序关联编辑界面 |
| 用关联程序编辑文件 | `F4` | 优先使用系统注册的程序 |
| 复制 / 移动（异步） | `F5` / `F6` | 后台队列，2 个工作协程，支持暂停 / 取消 / 状态栏 |
| 删除 | `Del` / `F8` | 带确认提示 |
| 配置编辑 | `Ctrl+P` | 编辑目录 / 排序规则 / 重置当前目录 |
| 帮助 | `F1` | 叠加层显示所有键位 |
| 搜索 | `/` | 按名称过滤可见条目 |
| 鼠标 | 任意 | 双击打开，单击选中 |

---

## 快速开始

下载最新发布版本（或本地自行编译，见[编译构建](#编译构建)）：

```bash
# Windows
dist\tcmd64.exe
dist\tcmd386.exe  # 用于 32 位系统

# macOS / Linux
./tcmd
```

默认启动目录为 `$HOME`（Unix）/ `%USERPROFILE%`（Windows）。
使用 `Ctrl+P` 可配置偏好的 `left_dir` / `right_dir` / `sort` 规则。

---

## 编译构建

### 环境要求

- Go 1.25.5+
- （可选）`swag` 用于 Windows 文档 —— 仅用于提取文档字符串，编译不需要
- （可选）ImageMagick `magick` CLI —— 仅生成文档截图时使用

### 构建

```bash
# 交叉编译为 64 位与 32 位 Windows
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/tcmd64.exe ./cmd/tcmd
CGO_ENABLED=0 GOOS=windows GOARCH=386 go build -o dist/tcmd386.exe ./cmd/tcmd

# 原生构建（产物在 dist/，发布统一使用 dist/）
CGO_ENABLED=0 go build -o dist/tcmd ./cmd/tcmd
```

### 测试

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go vet ./...
```

---

## 使用说明

### 运行

```bash
# 从某目录启动 —— 直接在该目录打开
./tcmd

# 或指定初始目录（左、右面板）
./tcmd /c /Users/me/projects
```

无需配置文件即可启动。首次运行两个面板默认都是当前目录。

### 键盘速查

| 键位 | 动作 |
|------|------|
| `↑` `↓` | 当前面板内上下移动光标 |
| `←` `→` | 切换活动面板 |
| `Enter` | 打开项目（目录 → 进入浏览，文件 → 用关联程序打开） |
| `Backspace` | 返回上一级 |
| `F1` | 显示帮助叠加层 |
| `F2` | 打开批量重命名预览（需先选中文件） |
| `F3` | 光标在文件上时调用关联程序打开 / 光标在目录上时打开目录树叠加层（可用时） |
| `F4` | 用关联程序编辑文件 |
| `F5` | 将选中项加入复制队列 |
| `F6` | 将选中项加入移动队列 |
| `F7` | 新建目录；`Alt+F7` 在当前路径打开独立命令行并复制路径到剪贴板 |
| `F8` / `Del` | 删除选中项 |
| `Space` | 切换选中状态 |
| `Ctrl+A` | 全选 |
| `Ctrl+E` | 打开关联编辑器（部分终端 Ctrl 组合键可能不送达，此时改用 `:assoc` 命令） |
| `Ctrl+T` | 新建标签页 |
| `Ctrl+W` | 关闭当前标签页 |
| `Ctrl+C` | 取消当前操作（复制 / 移动 / 重命名） |
| `/` | 开启按名称过滤搜索 |
| `:` | 开启命令输入（输入 `assoc` → 回车打开关联编辑器；`config` → 打开配置编辑器） |
| `Esc` | 关闭叠加层 / 清除搜索 |

### 命令模式

输入 `:` 开启命令输入。可用命令：

- `assoc` — 打开扩展名关联编辑器
- `config` — 打开配置编辑器
- `help` — 显示帮助叠加层

---

## 配置说明

配置自动持久化到与二进制同目录的 `tcmd.json`（Windows 与 Unix 均为此位置）。

### 字段结构

```json
{
  "panes": [
    { "tabs": ["C:\\Users\\me\\projects"], "active": 0 },
    { "tabs": ["C:\\Users\\me\\Downloads"], "active": 0 }
  ],
  "active": 0,
  "width": 120,
  "height": 30,
  "assoc": {
    "edit": {
      ".txt": "notepad.exe",
      ".log": "notepad.exe"
    },
    "view": {
      ".png": "mspaint.exe"
    },
    "open": {
      ".md": "notepad.exe"
    }
  }
}
```

- `panes` —— 左右两个面板的标签页栈（`tabs`）及当前激活标签（`active`）。
- `active` —— 当前活动面板（0 左 / 1 右）。
- `width` / `height` —— 最近一次窗口尺寸，用于恢复布局。
- `assoc` —— 扩展名关联表，主键为动作（`view` / `edit` / `open`），值为 `扩展名 → 命令` 映射。

### 编辑器关联

关联表持久化在 `tcmd.json` 的 `assoc` 键下（通过 `Ctrl+E` / `:assoc` 写入）。
每条记录的格式为 `<扩展名>: <命令 [参数]>`，例如：

```json
{
  "assoc": {
    "edit": {
      ".txt": "notepad.exe",
      ".log": "notepad.exe",
      ".md": "notepad.exe"
    },
    "view": {
      ".png": "mspaint.exe"
    }
  }
}
```

> **数据完整性说明**：配置文件写入采用原子写（临时文件 + 重命名），并对所有字符串做控制字符清洗（剔除 `\u0000` 等不可打印控制字符），避免输入法或异常读取导致 JSON 损坏。若发现旧配置中存在异常字符，删除对应条目重新保存即可。

---

## 扩展关联规则

关联解析顺序（命中即止）：

1. 系统级关联（Windows：`HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\<ext>\UserChoice` + `OpenWithProgids`；Unix：`xdg-open` / mimeapps.list）
2. tcmd 持久化的 `assoc` 表（在 `tcmd.json` 内）
3. 内置回退（Unix 下 `.txt` 用 `more`，Windows 下无回退）

新增或编辑方式：

1. 按 `Ctrl+E`（或输入 `:assoc` → 回车）打开关联编辑器
2. 按 `<扩展名> → <命令>` 格式新增 / 编辑条目
3. 保存（`Esc` 关闭）—— 改动立即持久化到 `tcmd.json`

---

## 架构设计

```
cmd/tcmd          — 主入口，参数解析
internal/tui/     — bubbletea 模型、键位处理、视图、叠加层
internal/fs/      — 平台无关的文件工具（os.Stat、os.ReadDir 等）
```

### 关键组件

- `tui/model.go` —— 核心 `model` 结构体；`update()` 驱动状态流转
- `tui/view.go` —— 渲染双面板布局、状态栏、叠加层
- `tui/ops.go` —— 文件操作封装（复制、移动、删除、重命名）
- `tui/job.go` —— 异步队列、工作协程、进度追踪
- `tui/tree.go` —— 目录树（F3）叠加层
- `tui/assoc.go` —— 扩展名关联编辑器
- `tui/batchrename.go` —— 批量重命名预览 / 应用
- `tui/config.go` —— 配置读写 + 编辑器叠加层
- `fs/fs.go` —— 平台无关文件 API
- `fs/fs_windows.go` / `fs_unix.go` —— 平台特定辅助（驱动器枚举、shell 关联）

---

## 注意事项与已知问题

- **Windows 11 ConPTY**：在某些宿主程序（PowerShell ISE、旧版 Windows Terminal）中，Ctrl 组合键可能传不到 `bubbletea`。`:assoc` 命令是终端无关的兜底方案。
- **CJK 输入**：输入框在 rune 层级正确处理输入法组合（逐 rune 移动光标），但视觉显示依赖终端渲染双宽字符的能力。依赖前请在你的真实终端中实测。
- **F3 目录树**：所有平台均可用；部分 Linux 配置下深层目录树渲染可能较慢，可用内置 `/` 搜索替代。
- **`dist/` 为规范构建输出目录**。请勿在项目根目录寻找 `./tcmd64.exe` —— 二进制统一输出到 `dist/` 以保持仓库整洁。
- **无需沙箱 / 无需管理员权限**：tcmd 完全运行在用户空间，从你的注册表 / `$XDG_CONFIG_HOME` 读取 shell 关联，而非系统级设置。

---

## 许可证

MIT
