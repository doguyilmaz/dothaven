// Package registry declares the config sources dothaven knows about and reads
// them into snapshot sections.
package registry

import (
	"encoding/json"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

type Kind int

const (
	File         Kind = iota // whole file → content
	FileMetadata             // file → {exists, lines} (no content)
	Dir                      // directory → item per entry
	JSONExtract              // JSON file → pairs from selected fields
)

type Sensitivity string

const (
	Low    Sensitivity = "low"
	Medium Sensitivity = "medium"
	High   Sensitivity = "high"
)

// Entry is a declarative config source. Paths is keyed by GOOS ("darwin",
// "linux", "windows"). Fields applies only to JSONExtract (empty = all keys).
type Entry struct {
	ID          string
	Name        string
	Paths       map[string]string
	Category    string
	Kind        Kind
	Fields      []string
	BackupDest  string
	Sensitivity Sensitivity
	Redact      func(string) string
}

// Entries is the full registry. Paths use ~ for home; %USERPROFILE%/%APPDATA%
// for Windows. (GOOS is "darwin"/"linux"/"windows".)
var Entries = []Entry{
	// AI: Claude
	{ID: "ai.claude.settings", Name: "Claude Settings", Category: "ai", Kind: JSONExtract, Fields: []string{"permissions", "enabledPlugins"}, BackupDest: "ai/claude/settings.json", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.claude/settings.json", "linux": "~/.claude/settings.json", "windows": "%USERPROFILE%/.claude/settings.json"}},
	{ID: "ai.claude.skills", Name: "Claude Skills", Category: "ai", Kind: Dir, BackupDest: "ai/claude/skills", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.claude/skills", "linux": "~/.claude/skills", "windows": "%USERPROFILE%/.claude/skills"}},
	{ID: "ai.claude.md", Name: "CLAUDE.md", Category: "ai", Kind: File, BackupDest: "ai/claude/CLAUDE.md", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.claude/CLAUDE.md", "linux": "~/.claude/CLAUDE.md", "windows": "%USERPROFILE%/.claude/CLAUDE.md"}},

	// AI: Cursor
	{ID: "ai.cursor.mcp", Name: "Cursor MCP Config", Category: "ai", Kind: File, BackupDest: "ai/cursor/mcp.json", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.cursor/mcp.json", "linux": "~/.cursor/mcp.json", "windows": "%USERPROFILE%/.cursor/mcp.json"}},
	{ID: "ai.cursor.skills", Name: "Cursor Skills", Category: "ai", Kind: Dir, BackupDest: "ai/cursor/skills", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.cursor/skills", "linux": "~/.cursor/skills", "windows": "%USERPROFILE%/.cursor/skills"}},

	// AI: Gemini
	{ID: "ai.gemini.settings", Name: "Gemini Settings", Category: "ai", Kind: JSONExtract, Fields: []string{}, BackupDest: "ai/gemini/settings.json", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.gemini/settings.json", "linux": "~/.gemini/settings.json", "windows": "%USERPROFILE%/.gemini/settings.json"}},
	{ID: "ai.gemini.skills", Name: "Gemini Skills", Category: "ai", Kind: Dir, BackupDest: "ai/gemini/skills", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.gemini/skills", "linux": "~/.gemini/skills", "windows": "%USERPROFILE%/.gemini/skills"}},
	{ID: "ai.gemini.md", Name: "GEMINI.md", Category: "ai", Kind: File, BackupDest: "ai/gemini/GEMINI.md", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.gemini/GEMINI.md", "linux": "~/.gemini/GEMINI.md", "windows": "%USERPROFILE%/.gemini/GEMINI.md"}},

	// AI: Windsurf
	{ID: "ai.windsurf.mcp", Name: "Windsurf MCP Config", Category: "ai", Kind: File, BackupDest: "ai/windsurf/mcp_config.json", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.codeium/windsurf/mcp_config.json", "linux": "~/.codeium/windsurf/mcp_config.json", "windows": "%USERPROFILE%/.codeium/windsurf/mcp_config.json"}},
	{ID: "ai.windsurf.skills", Name: "Windsurf Skills", Category: "ai", Kind: Dir, BackupDest: "ai/windsurf/skills", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.codeium/windsurf/skills", "linux": "~/.codeium/windsurf/skills", "windows": "%USERPROFILE%/.codeium/windsurf/skills"}},

	// Shell
	{ID: "shell.zshrc", Name: ".zshrc", Category: "shell", Kind: File, BackupDest: "shell/.zshrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.zshrc", "linux": "~/.zshrc"}},
	{ID: "shell.zprofile", Name: ".zprofile", Category: "shell", Kind: File, BackupDest: "shell/.zprofile", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.zprofile", "linux": "~/.zprofile"}},
	{ID: "shell.zshenv", Name: ".zshenv", Category: "shell", Kind: File, BackupDest: "shell/.zshenv", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.zshenv", "linux": "~/.zshenv"}},
	{ID: "shell.bash_profile", Name: ".bash_profile", Category: "shell", Kind: File, BackupDest: "shell/.bash_profile", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.bash_profile", "linux": "~/.bash_profile"}},
	{ID: "shell.bashrc", Name: ".bashrc", Category: "shell", Kind: File, BackupDest: "shell/.bashrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.bashrc", "linux": "~/.bashrc"}},

	// Git
	{ID: "git.config", Name: ".gitconfig", Category: "git", Kind: File, BackupDest: "git/.gitconfig", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.gitconfig", "linux": "~/.gitconfig", "windows": "%USERPROFILE%/.gitconfig"}},
	{ID: "git.ignore", Name: ".gitignore_global", Category: "git", Kind: File, BackupDest: "git/.gitignore_global", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.gitignore_global", "linux": "~/.gitignore_global", "windows": "%USERPROFILE%/.gitignore_global"}},
	{ID: "gh.config", Name: "GitHub CLI Config", Category: "git", Kind: File, BackupDest: "git/gh/config.yml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/gh/config.yml", "linux": "~/.config/gh/config.yml", "windows": "%APPDATA%/GitHub CLI/config.yml"}},

	// Editors
	{ID: "editor.zed", Name: "Zed Settings", Category: "editor", Kind: File, BackupDest: "editor/zed/settings.json", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/zed/settings.json", "linux": "~/.config/zed/settings.json", "windows": "%APPDATA%/Zed/settings.json"}},
	{ID: "editor.cursor", Name: "Cursor Settings", Category: "editor", Kind: File, BackupDest: "editor/cursor/settings.json", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Application Support/Cursor/User/settings.json", "linux": "~/.config/Cursor/User/settings.json", "windows": "%APPDATA%/Cursor/User/settings.json"}},
	{ID: "editor.nvim", Name: "Neovim Config", Category: "editor", Kind: File, BackupDest: "editor/nvim/init.lua", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/nvim/init.lua", "linux": "~/.config/nvim/init.lua", "windows": "%USERPROFILE%/AppData/Local/nvim/init.lua"}},
	{ID: "editor.vimrc", Name: ".vimrc", Category: "editor", Kind: File, BackupDest: "editor/.vimrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.vimrc", "linux": "~/.vimrc", "windows": "%USERPROFILE%/_vimrc"}},

	// Terminal
	{ID: "terminal.p10k", Name: ".p10k.zsh", Category: "terminal", Kind: FileMetadata, BackupDest: "terminal/.p10k.zsh", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.p10k.zsh", "linux": "~/.p10k.zsh"}},
	{ID: "terminal.tmux", Name: ".tmux.conf", Category: "terminal", Kind: File, BackupDest: "terminal/.tmux.conf", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.tmux.conf", "linux": "~/.tmux.conf"}},

	// SSH
	{ID: "ssh.config", Name: "SSH Config", Category: "ssh", Kind: File, BackupDest: "ssh/config", Sensitivity: Medium, Redact: scan.RedactSSHConfig, Paths: map[string]string{"darwin": "~/.ssh/config", "linux": "~/.ssh/config", "windows": "%USERPROFILE%/.ssh/config"}},

	// npm / bun
	{ID: "npm.config", Name: ".npmrc", Category: "npm", Kind: File, BackupDest: "npm/.npmrc", Sensitivity: High, Redact: scan.RedactNpmTokens, Paths: map[string]string{"darwin": "~/.npmrc", "linux": "~/.npmrc", "windows": "%USERPROFILE%/.npmrc"}},
	{ID: "bun.config", Name: ".bunfig.toml", Category: "bun", Kind: File, BackupDest: "bun/.bunfig.toml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.bunfig.toml", "linux": "~/.bunfig.toml", "windows": "%USERPROFILE%/.bunfig.toml"}},

	// Cloud CLIs
	{ID: "cloud.aws.config", Name: "AWS CLI config", Category: "cloud", Kind: File, BackupDest: "cloud/aws/config", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/.aws/config", "linux": "~/.aws/config", "windows": "%USERPROFILE%/.aws/config"}},
	{ID: "cloud.aws.credentials", Name: "AWS CLI credentials", Category: "cloud", Kind: File, BackupDest: "cloud/aws/credentials", Sensitivity: High, Paths: map[string]string{"darwin": "~/.aws/credentials", "linux": "~/.aws/credentials", "windows": "%USERPROFILE%/.aws/credentials"}},
	{ID: "cloud.gcloud.configurations", Name: "gcloud configurations", Category: "cloud", Kind: Dir, BackupDest: "cloud/gcloud/configurations", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/.config/gcloud/configurations", "linux": "~/.config/gcloud/configurations"}},
	{ID: "cloud.kube.config", Name: "kubeconfig", Category: "cloud", Kind: File, BackupDest: "cloud/kube/config", Sensitivity: High, Paths: map[string]string{"darwin": "~/.kube/config", "linux": "~/.kube/config", "windows": "%USERPROFILE%/.kube/config"}},
	{ID: "cloud.docker.config", Name: "Docker config", Category: "cloud", Kind: File, BackupDest: "cloud/docker/config.json", Sensitivity: High, Paths: map[string]string{"darwin": "~/.docker/config.json", "linux": "~/.docker/config.json", "windows": "%USERPROFILE%/.docker/config.json"}},

	// Secrets (carried encrypted) — declarative: a no-op until ~/.gnupg has real keys.
	{ID: "secrets.gnupg", Name: "GnuPG home", Category: "secrets", Kind: Dir, BackupDest: "secrets/gnupg", Sensitivity: High, Paths: map[string]string{"darwin": "~/.gnupg", "linux": "~/.gnupg"}},
}

// ResolvePath expands an entry's path template for the current OS ("" if the
// entry has no path for this platform).
func ResolvePath(e Entry, home string) string {
	tmpl, ok := e.Paths[runtime.GOOS]
	if !ok {
		return ""
	}
	return strings.Replace(tmpl, "~", home, 1)
}

// Collect reads every entry that exists on disk into a snapshot.
func Collect(env sys.Env, home string, redact bool, entries []Entry) snapshot.Snapshot {
	out := snapshot.Snapshot{}
	for _, e := range entries {
		path := ResolvePath(e, home)
		if path == "" {
			continue
		}
		switch e.Kind {
		case FileMetadata:
			b, err := env.ReadFile(path)
			if err != nil {
				continue
			}
			lines := strings.Count(string(b), "\n") + 1
			out[e.ID] = snapshot.Section{Pairs: map[string]string{"exists": "true", "lines": strconv.Itoa(lines)}}

		case File:
			b, err := env.ReadFile(path)
			if err != nil {
				continue
			}
			content := string(b)
			if redact && e.Redact != nil {
				content = e.Redact(content)
			}
			c := strings.TrimSpace(content)
			out[e.ID] = snapshot.Section{Content: &c}

		case Dir:
			names, err := env.ListDir(path)
			if err != nil || len(names) == 0 {
				continue
			}
			sort.Strings(names)
			items := make([]snapshot.Item, len(names))
			for i, n := range names {
				items[i] = snapshot.Item{Raw: n, Columns: []string{n}}
			}
			out[e.ID] = snapshot.Section{Items: items}

		case JSONExtract:
			b, err := env.ReadFile(path)
			if err != nil {
				continue
			}
			var data map[string]any
			if json.Unmarshal(b, &data) != nil {
				continue
			}
			if pairs := extractFields(data, e.Fields); len(pairs) > 0 {
				out[e.ID] = snapshot.Section{Pairs: pairs}
			}
		}
	}
	return out
}

func extractFields(data map[string]any, fields []string) map[string]string {
	keys := fields
	if len(keys) == 0 {
		for k := range data {
			keys = append(keys, k)
		}
	}
	pairs := map[string]string{}
	for _, f := range keys {
		v, ok := data[f]
		if !ok {
			continue
		}
		if obj, ok := v.(map[string]any); ok {
			for k, vv := range obj {
				pairs[k] = jsString(vv)
			}
		} else {
			pairs[f] = jsString(v)
		}
	}
	return pairs
}

// jsString mimics JS String(v) for scalars; arrays/objects fall back to JSON.
func jsString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case nil:
		return "null"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
