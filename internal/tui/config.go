package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sanitizeConfigString removes control characters (except \t, \n, \r) from a
// string to prevent JSON corruption (e.g., null bytes from buggy input or
// encoding issues). JSON strings cannot contain raw control chars; they must
// be escaped. If a string somehow gets raw control chars (e.g., from a buggy
// input method or corrupted read), this strips them before save.
func sanitizeConfigString(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// Keep printable and common whitespace; drop other control chars (including \u0000)
		if r >= 0x20 || r == '\t' || r == '\n' || r == '\r' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isValidConfigString reports whether s contains only valid JSON string characters
// (no raw control chars). Used for defensive checks on load.
func isValidConfigString(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

// paneState is the persisted shape of one side panel: the stack of open tab
// paths plus which tab is active. Each tab also carries cursor/offset so the
// UI can restore the user's position after a restart.
type paneState struct {
	Tabs   []string `json:"tabs"`
	Active int      `json:"active"`
	// TabCursor stores the cursor position for each tab, indexed by tab position.
	// Zero values are ignored on load (cursor defaults to 0).
	TabCursor []int `json:"tabCursor,omitempty"`
}

// AssocAction identifies which key gesture an association applies to.
type AssocAction string

const (
	// AssocView is the F3 (view) action.
	AssocView AssocAction = "view"
	// AssocEdit is the F4 (edit) action.
	AssocEdit AssocAction = "edit"
	// AssocOpen is the Enter (open) action.
	AssocOpen AssocAction = "open"
)

// assocKey normalizes an extension into the canonical map key: a lower-cased
// string with a single leading dot (e.g. ".TXT" -> ".txt", "txt" -> ".txt").
// An empty or dot-only input yields "".
func assocKey(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		return ""
	}
	return "." + ext
}

// Config is the on-disk session state. It is deliberately small and portable:
// just enough to restore "where you were" across launches. Stored as JSON next
// to the executable so the app stays a single green file with no external deps.
type Config struct {
	Panes  [2]paneState `json:"panes"`
	Active int          `json:"active"`
	Width  int          `json:"width"`
	Height int          `json:"height"`
	// Assoc maps each action (view/edit/open) to an extension->command map.
	// Keys are canonicalized (lowercase, leading dot) by SetAssoc; the loader
	// tolerates legacy/un-normalized keys by normalizing on access.
	Assoc map[string]map[string]string `json:"assoc,omitempty"`
}

// configPath returns the config file location: <exe-dir>/tcmd.json. Keeping it
// beside the exe preserves the green/portable nature of the build.
func configPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "tcmd.json"), nil
}

// LoadConfig reads tcmd.json. A missing or unreadable file is not an error: it
// simply means there is no prior session, and the caller should fall back to
// defaults.
func LoadConfig() (*Config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveConfig writes tcmd.json with indentation for human readability.
// Uses atomic write (temp file + rename) to prevent corruption on crash.
func SaveConfig(c *Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: write to temp file in same directory, then rename.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	// On Windows, os.Rename will replace existing file; on Unix it's atomic.
	return os.Rename(tmp, p)
}

// dirExists reports whether path is a readable directory.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// ApplyConfig rebuilds the two panes from a saved session. Invalid or missing
// tab paths fall back to the user home directory so a moved/deleted folder
// never crashes startup. Unknown fields are ignored.
func (m *model) ApplyConfig(c *Config) {
	if c == nil {
		return
	}
	for i := 0; i < 2; i++ {
		ps := c.Panes[i]
		if len(ps.Tabs) == 0 {
			continue
		}
		tabs := make([]*tab, 0, len(ps.Tabs))
		for j, pth := range ps.Tabs {
			if !dirExists(pth) {
				pth = homeDir()
			}
			t := newTab(pth)
			// Restore cursor if we have a saved value for this tab index.
			if j < len(ps.TabCursor) && ps.TabCursor[j] >= 0 {
				t.cursor = ps.TabCursor[j]
				if t.cursor >= len(t.entries) {
					t.cursor = len(t.entries) - 1
				}
				if t.cursor < 0 {
					t.cursor = 0
				}
			}
			tabs = append(tabs, t)
		}
		active := ps.Active
		if active < 0 || active >= len(tabs) {
			active = len(tabs) - 1
		}
		m.panes[i] = &pane{tabs: tabs, active: active}
	}
	if c.Active == 0 || c.Active == 1 {
		m.active = c.Active
	}
	if c.Width > 0 {
		m.width = c.Width
	}
	if c.Height > 0 {
		m.height = c.Height
	}
	m.loadAssoc(c.Assoc)
}

// saveConfig snapshots the current session to disk. It is called on structural
// changes (directory navigation, tab add/close/switch, pane switch) and on
// quit. Failures are non-fatal: a session file is a convenience, never a
// hard requirement, so we only note the error on stderr.
func (m *model) saveConfig() {
	c := &Config{Active: m.active, Width: m.width, Height: m.height}
	for i, p := range m.panes {
		ps := paneState{Active: p.active}
		for _, t := range p.tabs {
			ps.Tabs = append(ps.Tabs, t.path)
		}
		// Capture cursor positions so they survive a restart.
		for _, t := range p.tabs {
			ps.TabCursor = append(ps.TabCursor, t.cursor)
		}
		c.Panes[i] = ps
	}
	c.Assoc = m.exportAssoc()
	if err := SaveConfig(c); err != nil {
		fmt.Fprintln(os.Stderr, "tcmd: 保存配置失败:", err)
	}
}

// exportAssoc deep-copies the model's in-memory association map for saving.
// Returns nil when there are no associations so the JSON omits the field.
// Sanitizes command strings to remove any control characters (e.g., null bytes)
// that could corrupt the JSON output.
func (m *model) exportAssoc() map[string]map[string]string {
	if len(m.assoc) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(m.assoc))
	for action, m2 := range m.assoc {
		if len(m2) == 0 {
			continue
		}
		cp := make(map[string]string, len(m2))
		for k, v := range m2 {
			cp[k] = sanitizeConfigString(v)
		}
		out[action] = cp
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// loadAssoc copies the on-disk assoc map into memory, normalizing every key so
// legacy or hand-edited configs (mixed case, missing dot) still resolve.
func (m *model) loadAssoc(from map[string]map[string]string) {
	m.assoc = make(map[string]map[string]string)
	for action, m2 := range from {
		normAction := strings.ToLower(strings.TrimSpace(action))
		if normAction != string(AssocView) && normAction != string(AssocEdit) && normAction != string(AssocOpen) {
			continue
		}
		dst := make(map[string]string, len(m2))
		for k, v := range m2 {
			k = assocKey(k)
			if k == "" || strings.TrimSpace(v) == "" {
				continue
			}
			dst[k] = sanitizeConfigString(v)
		}
		if len(dst) > 0 {
			m.assoc[normAction] = dst
		}
	}
}

// SetAssoc binds (or rebinds) command to ext for the given action, persisting
// immediately. ext is normalized via assocKey; an empty ext is ignored.
// Command string is sanitized to remove control characters.
func (m *model) SetAssoc(action AssocAction, ext, command string) {
	key := assocKey(ext)
	command = sanitizeConfigString(strings.TrimSpace(command))
	if key == "" || command == "" {
		return
	}
	a := string(action)
	if m.assoc[a] == nil {
		m.assoc[a] = make(map[string]string)
	}
	m.assoc[a][key] = command
	m.saveConfig()
}

// DelAssoc removes the binding for ext under action (no-op if absent).
func (m *model) DelAssoc(action AssocAction, ext string) {
	key := assocKey(ext)
	if key == "" {
		return
	}
	a := string(action)
	if m.assoc[a] == nil {
		return
	}
	delete(m.assoc[a], key)
	if len(m.assoc[a]) == 0 {
		delete(m.assoc, a)
	}
	m.saveConfig()
}

// ResolveAssoc returns the command bound to ext for action, and whether a
// binding exists. ext is normalized, so ".TXT"/"txt" both match a ".txt" key.
func (m *model) ResolveAssoc(action AssocAction, ext string) (string, bool) {
	key := assocKey(ext)
	if key == "" {
		return "", false
	}
	cmd, ok := m.assoc[string(action)][key]
	return cmd, ok
}

// assocActions returns the three actions in display order for the editor.
func assocActions() []AssocAction {
	return []AssocAction{AssocView, AssocEdit, AssocOpen}
}
