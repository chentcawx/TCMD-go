# tcmd

A 32/64-bit, CGO-free, pure-Go cross-platform terminal file manager (TUI).

- **Go 1.25.5+**, zero CGo, builds natively on Windows / Linux / macOS
- ~5 MB binaries, no runtime dependencies
- Powered by [bubbletea](https://github.com/charmbracelet/bubbletea) + [lipgloss](https://github.com/charmbracelet/lipgloss)

---

## Table of Contents

- [Why this exists](#why-this-exists)
- [Features](#features)
- [Screenshots](#screenshots)
- [Quick Start](#quick-start)
- [Building](#building)
- [Usage](#usage)
- [Configuration](#configuration)
- [Extending associations](#extending-associations)
- [Architecture](#architecture)
- [Caveats & known issues](#caveats--known-issues)
- [License](#license)

---

## Why this exists

`cmd.exe` / `powershell.exe` are still the default shells on Windows — and they're bad at browsing.
`tcmd` fills the gap: a **fast, keyboard-driven file manager** that runs anywhere a terminal does,
with:

- a two-panel layout (the `cmd` / `mc` / `nnn` lineage)
- extensible file-type associations (open in the app the OS already has registered)
- async copy/move queue with pause / cancel and progress
- `batchrename` (multi-file rename with preview before writing)
- persistent config (paths, sort rule, cursor position) saved automatically between sessions

---

## Features

| Feature | Key | Details |
|---------|-----|---------|
| Dual-panel file browse | `←` `→` | Switch between left / right panel |
| Navigation | `↑` `↓` `Enter` `Backspace` | Open directories, go up |
| Directory tree (F3) | `F3` (on directory) | Quick-jump sidebar — works across platforms |
| Open file in associated app | `F3` / `F4` / `Enter` | Looks up OS extension association first, falls back to built-in preview |
| Open assoc editor | `Ctrl+E` / `:assoc` | Type `:` in any input field → `assoc` → Enter; opens the extension-editor overlay |
| Edit file in associated app | `F4` | Uses OS registration if available |
| Copy / Move (async) | `F5` / `F6` | Background queue, 2 workers, pause / cancel / status bar |
| Batch rename | `F7` (when selected) | Multi-select, preview, then write |
| Delete | `Del` | With confirmation prompt |
| Config editor | `Ctrl+P` | Edit paths / sort rule / cwd reset |
| Help | `F1` | Overlay showing all bindings |
| Search | `/` | Filter visible entries by name |
| Mouse | any | Double-click opens, single-click selects |

---

## Screenshots

Not available yet. Screenshots will be added to `docs/screenshots/` in a future release.

---

## Quick Start

Download the latest release (or build locally — see [Building](#building)):

```bash
# Windows
dist\tcmd64.exe
dist\tcmd386.exe  # for 32-bit systems

# macOS / Linux
./tcmd
```

By default, tcmd starts in `$HOME` (Unix) / `%USERPROFILE%` (Windows).
Use `Ctrl+P` to configure preferred `left_dir` / `right_dir` / `sort` rules.

---

## Building

### Requirements

- Go 1.25.5+
- (Optional) `swag` for Windows docs — used only for docstring extraction, not required to build
- (Optional) ImageMagick `magick` CLI — only used when generating documentation screenshots

### Build

```bash
# Cross-compile to 64-bit and 32-bit Windows
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/tcmd64.exe ./cmd/tcmd
CGO_ENABLED=0 GOOS=windows GOARCH=386 go build -o dist/tcmd386.exe ./cmd/tcmd

# Native build (leaves binary in project root; use dist/ for releases)
CGO_ENABLED=0 go build -o dist/tcmd ./cmd/tcmd
```

### Test

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go vet ./...
```

---

## Usage

### Running

```bash
# Launch from a directory — it opens there
./tcmd

# Or specify initial directories (left, right)
./tcmd /c /Users/me/projects
```

No config file is required to start. First-run config defaults to the current directory for both panels.

### Keyboard Reference

| Key | Action |
|-----|--------|
| `↑` `↓` | Move cursor up / down in current panel |
| `←` `→` | Switch active panel |
| `Enter` | Open item (directory → browse, file → associated app) |
| `Backspace` | Go up one level |
| `F1` | Show help overlay |
| `F2` | Create new directory |
| `F3` | Open directory tree (when on a dir) / open file in associated app (when on a file) |
| `F4` | Edit file in associated app |
| `F5` | Copy selected item(s) to async queue |
| `F6` | Move selected item(s) to async queue |
| `F7` | Open batch-rename preview |
| `F9` | Toggle between tree-view and list view (on directory nodes) |
| `Del` | Delete selected item(s) |
| `Space` | Toggle selection |
| `Ctrl+E` | Open association editor |
| `Ctrl+P` | Open config editor |
| `Ctrl+C` | Cancel the active operation (copy/move/rename) |
| `/` | Start name-filter search |
| `:` | Start command input (type `assoc` → Enter to open assoc editor) |
| `Esc` | Close overlay / clear search |

### Command Mode

Type `:` to open the command input. Available commands:

- `assoc` — open the extension association editor
- `config` — open the config editor
- `help` — show help overlay

---

## Configuration

Config is persisted automatically to `tcmd.json` in the same directory as the binary (Windows and Unix both use this location).

### Fields

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

- `panes` — tab stacks (`tabs`) and active tab index (`active`) for the left/right panels.
- `active` — which panel is active (0 left / 1 right).
- `width` / `height` — last window size, used to restore layout.
- `assoc` — extension association table; top-level key is the action (`view` / `edit` / `open`), value is an `extension → command` map.

### Editor Associations

The association map is persisted in `tcmd.json` under the `assoc` key (populated via `Ctrl+E` / `:assoc`).
Each entry is `<extension>: <command [args]>`, e.g.:

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

> **Data integrity note**: Config writes use atomic write (temp file + rename) and sanitize all
> strings to strip control characters (e.g. `\u0000`), preventing JSON corruption from IME or bad
> reads. If an old config shows stray control chars, delete that entry and re-save.

---

## Extending Associations

Associations are resolved in this order (first match wins):

1. OS-level association (Windows: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\<ext>\UserChoice` + `OpenWithProgids`; Unix: `xdg-open` / mimeapps.list)
2. tcmd's persisted `associations` map in `tcmd.json`
3. Built-in fallback (`more` on Unix for `.txt`, nothing on Windows)

To add or edit:

1. Press `Ctrl+E` (or type `:assoc` → Enter) to open the association editor
2. Add / edit entries in the form `<extension> → <command>`
3. Save (`Esc` to close) — changes are persisted to `tcmd.json` immediately

---

## Architecture

```
cmd/tcmd          — main entry, flags parsing
internal/tui/     — bubbletea model, key handlers, views, overlays
internal/fs/      — platform-agnostic file utilities (os.Stat, os.ReadDir, etc.)
```

### Key components

- `tui/model.go` — central `model` struct; `update()` drives state transitions
- `tui/view.go` — renders the two-panel layout, status bar, overlays
- `tui/ops.go` — file-operation wrappers (copy, move, delete, rename)
- `tui/job.go` — async queue, workers, progress tracking
- `tui/tree.go` — directory tree (F3) overlay
- `tui/assoc.go` — extension association editor
- `tui/batchrename.go` — multi-file rename preview / apply
- `tui/config.go` — config read/write + editor overlay
- `fs/fs.go` — platform-agnostic file API
- `fs/fs_windows.go` / `fs_unix.go` — platform-specific helpers (drive enumeration, shell assoc)

---

## Caveats & Known Issues

- **Windows 11 ConPTY**: Ctrl key combos may not reach `bubbletea` in some host applications
  (PowerShell ISE, Windows Terminal old builds). `:assoc` command is the terminal-independent
  fallback.
- **CJK input**: The input fields handle composition correctly at the rune level (rune-by-rune
  cursor), but the visual display depends on the terminal's ability to render double-width
  characters. Test in your actual terminal before relying on it.
- **F3 on directories**: Directory tree should work on all platforms; on some Linux configs,
  the tree render can be slow for very deep directories. Use the built-in `/` search as an
  alternative.
- **`dist/` is the canonical build output directory**. Do not look for `./tcmd64.exe` at the
  project root — binaries are emitted to `dist/` to keep the repo clean.
- **No sandbox / no admin required**: tcmd runs entirely in user space. It reads shell
  associations from your registry / `$XDG_CONFIG_HOME`, not from system-wide settings.

---

## License

MIT
