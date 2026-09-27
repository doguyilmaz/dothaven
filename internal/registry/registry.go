// Package registry declares the config sources dothaven knows about and reads
// them into snapshot sections.
package registry

import (
	"context"
	"encoding/json"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

// Selected applies the standard --only/--skip category filter shared by backup,
// restore, and export: --skip always wins; a non-empty --only restricts to its
// members. Single source of truth so the three commands can't drift.
func Selected(category string, only, skip []string) bool {
	if slices.Contains(skip, category) {
		return false
	}
	return len(only) == 0 || slices.Contains(only, category)
}

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
	// Exclude drops matching paths from a Dir entry's walk: caches, logs and
	// bundled binaries that live beside the config (see backup.Excluded).
	Exclude []string
}

// unix is the common case: the same ~-relative path on macOS and Linux.
func unix(p string) map[string]string { return map[string]string{"darwin": p, "linux": p} }

// appSupport is a path under the per-user application data folder, which is
// ~/Library/Application Support on macOS and ~/.config on Linux.
func appSupport(rel string) map[string]string {
	return map[string]string{"darwin": "~/Library/Application Support/" + rel, "linux": "~/.config/" + rel}
}

// Entries is the full registry. Paths use ~ for home; %USERPROFILE%/%APPDATA%
// for Windows. (GOOS is "darwin"/"linux"/"windows".)
var Entries = []Entry{
	// AI: Claude Code. Global (user-scope) config only — project-level
	// .claude/ and .mcp.json travel with each project.
	{ID: "ai.claude.settings", Name: "Claude Settings", Category: "ai", Kind: JSONExtract, Fields: []string{"permissions", "enabledPlugins", "extraKnownMarketplaces", "hooks", "statusLine", "model"}, BackupDest: "ai/claude/settings.json", Sensitivity: Medium,
		Paths: map[string]string{"darwin": "~/.claude/settings.json", "linux": "~/.claude/settings.json", "windows": "%USERPROFILE%/.claude/settings.json"}},
	// ~/.claude.json is where `claude mcp add --scope user` writes MCP
	// servers (their env often holds API keys), beside per-project state.
	{ID: "ai.claude.json", Name: "Claude Code state & user MCP servers", Category: "ai", Kind: JSONExtract, Fields: []string{"mcpServers"}, BackupDest: "ai/claude/claude.json", Sensitivity: Medium,
		Paths: map[string]string{"darwin": "~/.claude.json", "linux": "~/.claude.json", "windows": "%USERPROFILE%/.claude.json"}},
	{ID: "ai.claude.skills", Name: "Claude Skills", Category: "ai", Kind: Dir, BackupDest: "ai/claude/skills", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.claude/skills", "linux": "~/.claude/skills", "windows": "%USERPROFILE%/.claude/skills"}},
	{ID: "ai.claude.agents", Name: "Claude subagents", Category: "ai", Kind: Dir, BackupDest: "ai/claude/agents", Sensitivity: Low, Paths: unix("~/.claude/agents")},
	{ID: "ai.claude.commands", Name: "Claude slash commands", Category: "ai", Kind: Dir, BackupDest: "ai/claude/commands", Sensitivity: Low, Paths: unix("~/.claude/commands")},
	{ID: "ai.claude.hooks", Name: "Claude hook scripts", Category: "ai", Kind: Dir, BackupDest: "ai/claude/hooks", Sensitivity: Low, Paths: unix("~/.claude/hooks")},
	{ID: "ai.claude.output-styles", Name: "Claude output styles", Category: "ai", Kind: Dir, BackupDest: "ai/claude/output-styles", Sensitivity: Low, Paths: unix("~/.claude/output-styles")},
	// Installed plugins and the marketplaces they came from, git clones
	// included so a marketplace still updates after restore.
	{ID: "ai.claude.plugins", Name: "Claude plugins & marketplaces", Category: "ai", Kind: Dir, BackupDest: "ai/claude/plugins", Sensitivity: Low, Paths: unix("~/.claude/plugins")},
	{ID: "ai.claude.keybindings", Name: "Claude keybindings", Category: "ai", Kind: File, BackupDest: "ai/claude/keybindings.json", Sensitivity: Low, Paths: unix("~/.claude/keybindings.json")},
	{ID: "ai.claude.statusline", Name: "Claude status line script", Category: "ai", Kind: File, BackupDest: "ai/claude/statusline.sh", Sensitivity: Low, Paths: unix("~/.claude/statusline.sh")},
	// On Linux, Claude Code keeps its login here (macOS uses the Keychain).
	{ID: "ai.claude.credentials", Name: "Claude Code login", Category: "ai", Kind: File, BackupDest: "ai/claude/.credentials.json", Sensitivity: High, Paths: unix("~/.claude/.credentials.json")},
	{ID: "ai.claude.md", Name: "CLAUDE.md", Category: "ai", Kind: File, BackupDest: "ai/claude/CLAUDE.md", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.claude/CLAUDE.md", "linux": "~/.claude/CLAUDE.md", "windows": "%USERPROFILE%/.claude/CLAUDE.md"}},
	// Claude Desktop keeps its MCP servers in its own file.
	{ID: "ai.claude.desktop", Name: "Claude Desktop MCP config", Category: "ai", Kind: File, BackupDest: "ai/claude-desktop/claude_desktop_config.json", Sensitivity: Medium,
		Paths: map[string]string{"darwin": "~/Library/Application Support/Claude/claude_desktop_config.json", "linux": "~/.config/Claude/claude_desktop_config.json", "windows": "%APPDATA%/Claude/claude_desktop_config.json"}},

	// AI: Codex CLI (config.toml holds its MCP servers).
	{ID: "ai.codex.config", Name: "Codex config & MCP", Category: "ai", Kind: File, BackupDest: "ai/codex/config.toml", Sensitivity: Medium, Paths: unix("~/.codex/config.toml")},
	{ID: "ai.codex.agents", Name: "Codex AGENTS.md", Category: "ai", Kind: File, BackupDest: "ai/codex/AGENTS.md", Sensitivity: Low, Paths: unix("~/.codex/AGENTS.md")},
	{ID: "ai.codex.prompts", Name: "Codex prompts", Category: "ai", Kind: Dir, BackupDest: "ai/codex/prompts", Sensitivity: Low, Paths: unix("~/.codex/prompts")},
	{ID: "ai.codex.skills", Name: "Codex skills", Category: "ai", Kind: Dir, BackupDest: "ai/codex/skills", Sensitivity: Low, Paths: unix("~/.codex/skills")},
	{ID: "ai.codex.rules", Name: "Codex command rules", Category: "ai", Kind: Dir, BackupDest: "ai/codex/rules", Sensitivity: Low, Paths: unix("~/.codex/rules")},
	{ID: "ai.codex.auth", Name: "Codex login", Category: "ai", Kind: File, BackupDest: "ai/codex/auth.json", Sensitivity: High, Paths: unix("~/.codex/auth.json")},

	// AI: Cursor
	{ID: "ai.cursor.mcp", Name: "Cursor MCP Config", Category: "ai", Kind: File, BackupDest: "ai/cursor/mcp.json", Sensitivity: Medium,
		Paths: map[string]string{"darwin": "~/.cursor/mcp.json", "linux": "~/.cursor/mcp.json", "windows": "%USERPROFILE%/.cursor/mcp.json"}},
	{ID: "ai.cursor.skills", Name: "Cursor Skills", Category: "ai", Kind: Dir, BackupDest: "ai/cursor/skills", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.cursor/skills", "linux": "~/.cursor/skills", "windows": "%USERPROFILE%/.cursor/skills"}},
	{ID: "ai.cursor.commands", Name: "Cursor commands", Category: "ai", Kind: Dir, BackupDest: "ai/cursor/commands", Sensitivity: Low, Paths: unix("~/.cursor/commands")},
	{ID: "ai.cursor.rules", Name: "Cursor rules", Category: "ai", Kind: Dir, BackupDest: "ai/cursor/rules", Sensitivity: Low, Paths: unix("~/.cursor/rules")},
	{ID: "ai.cursor.agents", Name: "Cursor agents", Category: "ai", Kind: Dir, BackupDest: "ai/cursor/agents", Sensitivity: Low, Paths: unix("~/.cursor/agents")},

	// AI: Gemini CLI
	{ID: "ai.gemini.settings", Name: "Gemini Settings", Category: "ai", Kind: JSONExtract, Fields: []string{}, BackupDest: "ai/gemini/settings.json", Sensitivity: Medium,
		Paths: map[string]string{"darwin": "~/.gemini/settings.json", "linux": "~/.gemini/settings.json", "windows": "%USERPROFILE%/.gemini/settings.json"}},
	{ID: "ai.gemini.skills", Name: "Gemini Skills", Category: "ai", Kind: Dir, BackupDest: "ai/gemini/skills", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.gemini/skills", "linux": "~/.gemini/skills", "windows": "%USERPROFILE%/.gemini/skills"}},
	{ID: "ai.gemini.md", Name: "GEMINI.md", Category: "ai", Kind: File, BackupDest: "ai/gemini/GEMINI.md", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.gemini/GEMINI.md", "linux": "~/.gemini/GEMINI.md", "windows": "%USERPROFILE%/.gemini/GEMINI.md"}},
	{ID: "ai.gemini.commands", Name: "Gemini commands", Category: "ai", Kind: Dir, BackupDest: "ai/gemini/commands", Sensitivity: Low, Paths: unix("~/.gemini/commands")},
	{ID: "ai.gemini.extensions", Name: "Gemini extensions", Category: "ai", Kind: Dir, BackupDest: "ai/gemini/extensions", Sensitivity: Low, Paths: unix("~/.gemini/extensions"), Exclude: []string{"node_modules"}},
	{ID: "ai.gemini.oauth", Name: "Gemini login", Category: "ai", Kind: File, BackupDest: "ai/gemini/oauth_creds.json", Sensitivity: High, Paths: unix("~/.gemini/oauth_creds.json")},
	{ID: "ai.gemini.env", Name: "Gemini .env (API key)", Category: "ai", Kind: File, BackupDest: "ai/gemini/.env", Sensitivity: High, Paths: unix("~/.gemini/.env")},

	// AI: Windsurf
	{ID: "ai.windsurf.mcp", Name: "Windsurf MCP Config", Category: "ai", Kind: File, BackupDest: "ai/windsurf/mcp_config.json", Sensitivity: Medium,
		Paths: map[string]string{"darwin": "~/.codeium/windsurf/mcp_config.json", "linux": "~/.codeium/windsurf/mcp_config.json", "windows": "%USERPROFILE%/.codeium/windsurf/mcp_config.json"}},
	{ID: "ai.windsurf.skills", Name: "Windsurf Skills", Category: "ai", Kind: Dir, BackupDest: "ai/windsurf/skills", Sensitivity: Low,
		Paths: map[string]string{"darwin": "~/.codeium/windsurf/skills", "linux": "~/.codeium/windsurf/skills", "windows": "%USERPROFILE%/.codeium/windsurf/skills"}},
	{ID: "ai.windsurf.rules", Name: "Windsurf global rules", Category: "ai", Kind: File, BackupDest: "ai/windsurf/global_rules.md", Sensitivity: Low, Paths: unix("~/.codeium/windsurf/memories/global_rules.md")},

	// AI: other agents and their MCP configs.
	{ID: "ai.vscode.mcp", Name: "VS Code MCP servers", Category: "ai", Kind: File, BackupDest: "ai/vscode/mcp.json", Sensitivity: Medium, Paths: appSupport("Code/User/mcp.json")},
	{ID: "ai.opencode", Name: "opencode config, agents & commands", Category: "ai", Kind: Dir, BackupDest: "ai/opencode", Sensitivity: Medium, Paths: unix("~/.config/opencode"), Exclude: []string{"node_modules"}},
	{ID: "ai.copilot.mcp", Name: "Copilot CLI MCP config", Category: "ai", Kind: File, BackupDest: "ai/copilot/mcp-config.json", Sensitivity: Medium, Paths: unix("~/.copilot/mcp-config.json")},
	{ID: "ai.copilot.config", Name: "Copilot CLI config", Category: "ai", Kind: File, BackupDest: "ai/copilot/config.json", Sensitivity: Medium, Paths: unix("~/.copilot/config.json")},
	{ID: "ai.continue.yaml", Name: "Continue config", Category: "ai", Kind: File, BackupDest: "ai/continue/config.yaml", Sensitivity: Medium, Paths: unix("~/.continue/config.yaml")},
	{ID: "ai.continue.json", Name: "Continue config (json)", Category: "ai", Kind: File, BackupDest: "ai/continue/config.json", Sensitivity: Medium, Paths: unix("~/.continue/config.json")},
	{ID: "ai.aider", Name: "aider config", Category: "ai", Kind: File, BackupDest: "ai/aider/.aider.conf.yml", Sensitivity: Medium, Paths: unix("~/.aider.conf.yml")},
	{ID: "ai.goose", Name: "Goose config", Category: "ai", Kind: File, BackupDest: "ai/goose/config.yaml", Sensitivity: Medium, Paths: unix("~/.config/goose/config.yaml")},
	{ID: "ai.kiro.mcp", Name: "Kiro MCP config", Category: "ai", Kind: File, BackupDest: "ai/kiro/mcp.json", Sensitivity: Medium, Paths: unix("~/.kiro/settings/mcp.json")},
	{ID: "ai.kiro.steering", Name: "Kiro steering", Category: "ai", Kind: Dir, BackupDest: "ai/kiro/steering", Sensitivity: Low, Paths: unix("~/.kiro/steering")},
	{ID: "ai.amp", Name: "Amp settings", Category: "ai", Kind: File, BackupDest: "ai/amp/settings.json", Sensitivity: Medium, Paths: unix("~/.config/amp/settings.json")},
	{ID: "ai.qwen", Name: "Qwen Code settings", Category: "ai", Kind: File, BackupDest: "ai/qwen/settings.json", Sensitivity: Medium, Paths: unix("~/.qwen/settings.json")},
	// Cline and Roo Code are VS Code extensions; their MCP servers live in the
	// editor's extension storage, not in the editor settings.
	{ID: "ai.cline.mcp", Name: "Cline MCP servers", Category: "ai", Kind: File, BackupDest: "ai/cline/cline_mcp_settings.json", Sensitivity: Medium, Paths: appSupport("Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json")},
	{ID: "ai.cline.cursor.mcp", Name: "Cline MCP servers (in Cursor)", Category: "ai", Kind: File, BackupDest: "ai/cline/cursor/cline_mcp_settings.json", Sensitivity: Medium, Paths: appSupport("Cursor/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json")},
	{ID: "ai.cline.rules", Name: "Cline global rules", Category: "ai", Kind: Dir, BackupDest: "ai/cline/Rules", Sensitivity: Low, Paths: unix("~/Documents/Cline/Rules")},
	{ID: "ai.cline.workflows", Name: "Cline global workflows", Category: "ai", Kind: Dir, BackupDest: "ai/cline/Workflows", Sensitivity: Low, Paths: unix("~/Documents/Cline/Workflows")},
	{ID: "ai.roo.mcp", Name: "Roo Code MCP servers", Category: "ai", Kind: File, BackupDest: "ai/roo/mcp_settings.json", Sensitivity: Medium, Paths: appSupport("Code/User/globalStorage/rooveterinaryinc.roo-cline/settings/mcp_settings.json")},
	{ID: "ai.roo.rules", Name: "Roo Code global rules", Category: "ai", Kind: Dir, BackupDest: "ai/roo/rules", Sensitivity: Low, Paths: unix("~/.roo/rules")},
	{ID: "ai.lmstudio.mcp", Name: "LM Studio MCP config", Category: "ai", Kind: File, BackupDest: "ai/lmstudio/mcp.json", Sensitivity: Medium, Paths: unix("~/.lmstudio/mcp.json")},

	// Shell
	{ID: "shell.zshrc", Name: ".zshrc", Category: "shell", Kind: File, BackupDest: "shell/.zshrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.zshrc", "linux": "~/.zshrc"}},
	{ID: "shell.zprofile", Name: ".zprofile", Category: "shell", Kind: File, BackupDest: "shell/.zprofile", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.zprofile", "linux": "~/.zprofile"}},
	{ID: "shell.zshenv", Name: ".zshenv", Category: "shell", Kind: File, BackupDest: "shell/.zshenv", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.zshenv", "linux": "~/.zshenv"}},
	{ID: "shell.bash_profile", Name: ".bash_profile", Category: "shell", Kind: File, BackupDest: "shell/.bash_profile", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.bash_profile", "linux": "~/.bash_profile"}},
	{ID: "shell.bashrc", Name: ".bashrc", Category: "shell", Kind: File, BackupDest: "shell/.bashrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.bashrc", "linux": "~/.bashrc"}},
	{ID: "shell.zlogin", Name: ".zlogin", Category: "shell", Kind: File, BackupDest: "shell/.zlogin", Sensitivity: Low, Paths: unix("~/.zlogin")},
	{ID: "shell.zlogout", Name: ".zlogout", Category: "shell", Kind: File, BackupDest: "shell/.zlogout", Sensitivity: Low, Paths: unix("~/.zlogout")},
	{ID: "shell.bash_aliases", Name: ".bash_aliases", Category: "shell", Kind: File, BackupDest: "shell/.bash_aliases", Sensitivity: Low, Paths: unix("~/.bash_aliases")},
	{ID: "shell.bash_logout", Name: ".bash_logout", Category: "shell", Kind: File, BackupDest: "shell/.bash_logout", Sensitivity: Low, Paths: unix("~/.bash_logout")},
	// fzf's install script writes these and .zshrc sources them; a restored
	// .zshrc without them errors on every new shell.
	{ID: "shell.fzf.zsh", Name: ".fzf.zsh", Category: "shell", Kind: File, BackupDest: "shell/.fzf.zsh", Sensitivity: Low, Paths: unix("~/.fzf.zsh")},
	{ID: "shell.fzf.bash", Name: ".fzf.bash", Category: "shell", Kind: File, BackupDest: "shell/.fzf.bash", Sensitivity: Low, Paths: unix("~/.fzf.bash")},
	{ID: "shell.zsh.dir", Name: "~/.zsh", Category: "shell", Kind: Dir, BackupDest: "shell/zsh", Sensitivity: Low, Paths: unix("~/.zsh"), Exclude: []string{".zcompdump*", "*.zwc", ".zsh_history"}},
	{ID: "shell.zsh.xdg", Name: "XDG zsh config", Category: "shell", Kind: Dir, BackupDest: "shell/zsh-xdg", Sensitivity: Low, Paths: unix("~/.config/zsh"), Exclude: []string{".zcompdump*", "*.zwc", ".zsh_history", ".zsh_sessions"}},

	// Git
	{ID: "git.config", Name: ".gitconfig", Category: "git", Kind: File, BackupDest: "git/.gitconfig", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.gitconfig", "linux": "~/.gitconfig", "windows": "%USERPROFILE%/.gitconfig"}},
	{ID: "git.ignore", Name: ".gitignore_global", Category: "git", Kind: File, BackupDest: "git/.gitignore_global", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.gitignore_global", "linux": "~/.gitignore_global", "windows": "%USERPROFILE%/.gitignore_global"}},
	{ID: "gh.config", Name: "GitHub CLI Config", Category: "git", Kind: File, BackupDest: "git/gh/config.yml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/gh/config.yml", "linux": "~/.config/gh/config.yml", "windows": "%APPDATA%/GitHub CLI/config.yml"}},

	// Editors
	{ID: "editor.zed", Name: "Zed Settings", Category: "editor", Kind: File, BackupDest: "editor/zed/settings.json", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/zed/settings.json", "linux": "~/.config/zed/settings.json", "windows": "%APPDATA%/Zed/settings.json"}},
	{ID: "editor.cursor", Name: "Cursor Settings", Category: "editor", Kind: File, BackupDest: "editor/cursor/settings.json", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Application Support/Cursor/User/settings.json", "linux": "~/.config/Cursor/User/settings.json", "windows": "%APPDATA%/Cursor/User/settings.json"}},
	{ID: "editor.cursor.keybindings", Name: "Cursor Keybindings", Category: "editor", Kind: File, BackupDest: "editor/cursor/keybindings.json", Sensitivity: Low, Paths: appSupport("Cursor/User/keybindings.json")},
	{ID: "editor.cursor.snippets", Name: "Cursor Snippets", Category: "editor", Kind: Dir, BackupDest: "editor/cursor/snippets", Sensitivity: Low, Paths: appSupport("Cursor/User/snippets")},
	{ID: "editor.zed.keymap", Name: "Zed Keymap", Category: "editor", Kind: File, BackupDest: "editor/zed/keymap.json", Sensitivity: Low, Paths: unix("~/.config/zed/keymap.json")},
	{ID: "editor.zed.themes", Name: "Zed Themes", Category: "editor", Kind: Dir, BackupDest: "editor/zed/themes", Sensitivity: Low, Paths: unix("~/.config/zed/themes")},
	{ID: "editor.zed.snippets", Name: "Zed Snippets", Category: "editor", Kind: Dir, BackupDest: "editor/zed/snippets", Sensitivity: Low, Paths: unix("~/.config/zed/snippets")},
	{ID: "editor.windsurf", Name: "Windsurf Settings", Category: "editor", Kind: File, BackupDest: "editor/windsurf/settings.json", Sensitivity: Low, Paths: appSupport("Windsurf/User/settings.json")},
	{ID: "editor.windsurf.keybindings", Name: "Windsurf Keybindings", Category: "editor", Kind: File, BackupDest: "editor/windsurf/keybindings.json", Sensitivity: Low, Paths: appSupport("Windsurf/User/keybindings.json")},
	{ID: "editor.nvim", Name: "Neovim Config", Category: "editor", Kind: Dir, BackupDest: "editor/nvim", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/nvim", "linux": "~/.config/nvim", "windows": "%USERPROFILE%/AppData/Local/nvim"}},
	{ID: "editor.vim.dir", Name: "~/.vim (without plugins)", Category: "editor", Kind: Dir, BackupDest: "editor/vim", Sensitivity: Low, Paths: unix("~/.vim"), Exclude: []string{"plugged", "bundle", "pack", "undo", "swap", "backup", ".netrwhist"}},
	{ID: "editor.emacs", Name: "Emacs init", Category: "editor", Kind: File, BackupDest: "editor/.emacs", Sensitivity: Low, Paths: unix("~/.emacs")},
	{ID: "editor.emacs.d", Name: "~/.emacs.d (config only)", Category: "editor", Kind: Dir, BackupDest: "editor/emacs.d", Sensitivity: Low, Paths: unix("~/.emacs.d"), Exclude: []string{"elpa", "eln-cache", "straight", ".cache", "auto-save-list", "var", "*.elc", "transient"}},
	{ID: "editor.vimrc", Name: ".vimrc", Category: "editor", Kind: File, BackupDest: "editor/.vimrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.vimrc", "linux": "~/.vimrc", "windows": "%USERPROFILE%/_vimrc"}},

	// Terminal
	{ID: "terminal.p10k", Name: ".p10k.zsh", Category: "terminal", Kind: FileMetadata, BackupDest: "terminal/.p10k.zsh", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.p10k.zsh", "linux": "~/.p10k.zsh"}},
	{ID: "terminal.tmux", Name: ".tmux.conf", Category: "terminal", Kind: File, BackupDest: "terminal/.tmux.conf", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.tmux.conf", "linux": "~/.tmux.conf"}},
	{ID: "terminal.tmux.xdg", Name: "XDG tmux config", Category: "terminal", Kind: Dir, BackupDest: "terminal/tmux", Sensitivity: Low, Paths: unix("~/.config/tmux"), Exclude: []string{"plugins"}},
	{ID: "terminal.iterm2.profiles", Name: "iTerm2 dynamic profiles", Category: "terminal", Kind: Dir, BackupDest: "terminal/iterm2/DynamicProfiles", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Application Support/iTerm2/DynamicProfiles"}},

	// SSH
	{ID: "ssh.config", Name: "SSH Config", Category: "ssh", Kind: File, BackupDest: "ssh/config", Sensitivity: Medium, Redact: scan.RedactSSHConfig, Paths: map[string]string{"darwin": "~/.ssh/config", "linux": "~/.ssh/config", "windows": "%USERPROFILE%/.ssh/config"}},
	// The whole of ~/.ssh: keys, known_hosts, config.d. High, so a plaintext
	// backup leaves it out (and says so) and an encrypted one carries it.
	// Sockets (ControlPath, agent) are skipped by the walk.
	{ID: "ssh.dir", Name: "SSH keys & known hosts", Category: "ssh", Kind: Dir, BackupDest: "ssh", Sensitivity: High, Paths: unix("~/.ssh"), Exclude: []string{"*.sock", "sockets", "cm-*", "control-*"}},

	// npm / bun
	{ID: "npm.config", Name: ".npmrc", Category: "npm", Kind: File, BackupDest: "npm/.npmrc", Sensitivity: High, Redact: scan.RedactNpmTokens, Paths: map[string]string{"darwin": "~/.npmrc", "linux": "~/.npmrc", "windows": "%USERPROFILE%/.npmrc"}},
	{ID: "bun.config", Name: ".bunfig.toml", Category: "bun", Kind: File, BackupDest: "bun/.bunfig.toml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.bunfig.toml", "linux": "~/.bunfig.toml", "windows": "%USERPROFILE%/.bunfig.toml"}},

	// Cloud CLIs
	{ID: "cloud.aws.config", Name: "AWS CLI config", Category: "cloud", Kind: File, BackupDest: "cloud/aws/config", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/.aws/config", "linux": "~/.aws/config", "windows": "%USERPROFILE%/.aws/config"}},
	{ID: "cloud.aws.credentials", Name: "AWS CLI credentials", Category: "cloud", Kind: File, BackupDest: "cloud/aws/credentials", Sensitivity: High, Paths: map[string]string{"darwin": "~/.aws/credentials", "linux": "~/.aws/credentials", "windows": "%USERPROFILE%/.aws/credentials"}},
	{ID: "cloud.gcloud.configurations", Name: "gcloud configurations", Category: "cloud", Kind: Dir, BackupDest: "cloud/gcloud/configurations", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/.config/gcloud/configurations", "linux": "~/.config/gcloud/configurations"}},
	{ID: "cloud.kube.config", Name: "kubeconfig", Category: "cloud", Kind: File, BackupDest: "cloud/kube/config", Sensitivity: High, Paths: map[string]string{"darwin": "~/.kube/config", "linux": "~/.kube/config", "windows": "%USERPROFILE%/.kube/config"}},
	// Extra kubeconfigs beside the main one (many people keep one per cluster).
	{ID: "cloud.kube.dir", Name: "kube configs", Category: "cloud", Kind: Dir, BackupDest: "cloud/kube", Sensitivity: High, Paths: unix("~/.kube"), Exclude: []string{"cache", "http-cache", "kubens", "kubectx"}},
	{ID: "cloud.docker.daemon", Name: "Docker daemon.json", Category: "cloud", Kind: File, BackupDest: "cloud/docker/daemon.json", Sensitivity: Low, Paths: unix("~/.docker/daemon.json")},
	{ID: "cloud.gcloud.adc", Name: "gcloud application default credentials", Category: "cloud", Kind: File, BackupDest: "cloud/gcloud/application_default_credentials.json", Sensitivity: High, Paths: unix("~/.config/gcloud/application_default_credentials.json")},
	{ID: "cloud.firebase", Name: "Firebase CLI login", Category: "cloud", Kind: File, BackupDest: "cloud/firebase/firebase-tools.json", Sensitivity: High, Paths: unix("~/.config/configstore/firebase-tools.json")},
	{ID: "cloud.docker.config", Name: "Docker config", Category: "cloud", Kind: File, BackupDest: "cloud/docker/config.json", Sensitivity: High, Paths: map[string]string{"darwin": "~/.docker/config.json", "linux": "~/.docker/config.json", "windows": "%USERPROFILE%/.docker/config.json"}},

	// Cloud CLIs (more) — every credential-bearing entry is High so chezmoi-export
	// encrypts it; with no redactor it is also excluded from a plaintext backup.
	{ID: "cloud.azure", Name: "Azure CLI", Category: "cloud", Kind: Dir, BackupDest: "cloud/azure", Sensitivity: High, Paths: map[string]string{"darwin": "~/.azure", "linux": "~/.azure", "windows": "%USERPROFILE%/.azure"}, Exclude: []string{"cliextensions", "logs", "commands", "telemetry", "*.log"}},
	{ID: "cloud.oci", Name: "Oracle Cloud (OCI)", Category: "cloud", Kind: Dir, BackupDest: "cloud/oci", Sensitivity: High, Paths: map[string]string{"darwin": "~/.oci", "linux": "~/.oci"}},
	{ID: "cloud.digitalocean", Name: "DigitalOcean (doctl)", Category: "cloud", Kind: File, BackupDest: "cloud/doctl/config.yaml", Sensitivity: High, Paths: map[string]string{"darwin": "~/Library/Application Support/doctl/config.yaml", "linux": "~/.config/doctl/config.yaml"}},
	{ID: "cloud.fly", Name: "Fly.io", Category: "cloud", Kind: Dir, BackupDest: "cloud/fly", Sensitivity: High, Paths: map[string]string{"darwin": "~/.fly", "linux": "~/.fly"}, Exclude: []string{"bin"}},
	{ID: "cloud.linode", Name: "Linode CLI", Category: "cloud", Kind: File, BackupDest: "cloud/linode-cli", Sensitivity: High, Paths: map[string]string{"darwin": "~/.config/linode-cli", "linux": "~/.config/linode-cli"}},
	{ID: "cloud.hetzner", Name: "Hetzner (hcloud)", Category: "cloud", Kind: File, BackupDest: "cloud/hcloud/cli.toml", Sensitivity: High, Paths: map[string]string{"darwin": "~/.config/hcloud/cli.toml", "linux": "~/.config/hcloud/cli.toml"}},
	{ID: "cloud.vercel", Name: "Vercel CLI", Category: "cloud", Kind: File, BackupDest: "cloud/vercel/auth.json", Sensitivity: High, Paths: map[string]string{"darwin": "~/Library/Application Support/com.vercel.cli/auth.json", "linux": "~/.local/share/com.vercel.cli/auth.json"}},
	{ID: "cloud.netlify", Name: "Netlify CLI", Category: "cloud", Kind: File, BackupDest: "cloud/netlify/config.json", Sensitivity: High, Paths: map[string]string{"darwin": "~/.config/netlify/config.json", "linux": "~/.config/netlify/config.json"}},
	{ID: "cloud.supabase", Name: "Supabase CLI", Category: "cloud", Kind: Dir, BackupDest: "cloud/supabase", Sensitivity: High, Paths: map[string]string{"darwin": "~/.supabase", "linux": "~/.supabase"}, Exclude: []string{"bin", "templates"}},
	{ID: "cloud.stripe", Name: "Stripe CLI", Category: "cloud", Kind: File, BackupDest: "cloud/stripe/config.toml", Sensitivity: High, Paths: map[string]string{"darwin": "~/.config/stripe/config.toml", "linux": "~/.config/stripe/config.toml"}},
	{ID: "cloud.railway", Name: "Railway CLI", Category: "cloud", Kind: File, BackupDest: "cloud/railway/config.json", Sensitivity: High, Paths: map[string]string{"darwin": "~/.railway/config.json", "linux": "~/.railway/config.json"}},
	{ID: "cloud.terraform", Name: "Terraform Cloud creds", Category: "cloud", Kind: File, BackupDest: "cloud/terraform/credentials.tfrc.json", Sensitivity: High, Paths: map[string]string{"darwin": "~/.terraform.d/credentials.tfrc.json", "linux": "~/.terraform.d/credentials.tfrc.json"}},
	{ID: "cloud.pulumi", Name: "Pulumi creds", Category: "cloud", Kind: File, BackupDest: "cloud/pulumi/credentials.json", Sensitivity: High, Paths: map[string]string{"darwin": "~/.pulumi/credentials.json", "linux": "~/.pulumi/credentials.json"}},
	{ID: "cloud.cloudflared", Name: "Cloudflared", Category: "cloud", Kind: Dir, BackupDest: "cloud/cloudflared", Sensitivity: High, Paths: map[string]string{"darwin": "~/.cloudflared", "linux": "~/.cloudflared"}},

	// DevOps / k8s tooling
	{ID: "devops.helm", Name: "Helm repositories", Category: "devops", Kind: File, BackupDest: "devops/helm/repositories.yaml", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/Library/Preferences/helm/repositories.yaml", "linux": "~/.config/helm/repositories.yaml"}},
	{ID: "devops.k9s", Name: "k9s config", Category: "devops", Kind: File, BackupDest: "devops/k9s/config.yaml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Application Support/k9s/config.yaml", "linux": "~/.config/k9s/config.yaml"}},
	{ID: "devops.colima", Name: "Colima config", Category: "devops", Kind: File, BackupDest: "devops/colima/colima.yaml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.colima/default/colima.yaml", "linux": "~/.colima/default/colima.yaml"}},
	{ID: "devops.podman", Name: "Podman config", Category: "devops", Kind: Dir, BackupDest: "devops/podman", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/.config/containers", "linux": "~/.config/containers"}},

	// Build tools (may hold repository credentials → High)
	{ID: "build.maven", Name: "Maven settings", Category: "build", Kind: File, BackupDest: "build/maven/settings.xml", Sensitivity: High, Paths: map[string]string{"darwin": "~/.m2/settings.xml", "linux": "~/.m2/settings.xml", "windows": "%USERPROFILE%/.m2/settings.xml"}},
	{ID: "build.gradle", Name: "Gradle properties", Category: "build", Kind: File, BackupDest: "build/gradle/gradle.properties", Sensitivity: High, Paths: map[string]string{"darwin": "~/.gradle/gradle.properties", "linux": "~/.gradle/gradle.properties", "windows": "%USERPROFILE%/.gradle/gradle.properties"}},

	// Databases
	{ID: "db.pgpass", Name: ".pgpass", Category: "db", Kind: File, BackupDest: "db/.pgpass", Sensitivity: High, Paths: map[string]string{"darwin": "~/.pgpass", "linux": "~/.pgpass", "windows": "%APPDATA%/postgresql/pgpass.conf"}},
	{ID: "db.mycnf", Name: ".my.cnf", Category: "db", Kind: File, BackupDest: "db/.my.cnf", Sensitivity: High, Paths: map[string]string{"darwin": "~/.my.cnf", "linux": "~/.my.cnf"}},
	{ID: "db.psqlrc", Name: ".psqlrc", Category: "db", Kind: File, BackupDest: "db/.psqlrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.psqlrc", "linux": "~/.psqlrc"}},
	{ID: "db.sqliterc", Name: ".sqliterc", Category: "db", Kind: File, BackupDest: "db/.sqliterc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.sqliterc", "linux": "~/.sqliterc"}},

	// Editors (more) — VS Code settings, Helix, Doom, Sublime, editorconfig
	{ID: "editor.vscode.settings", Name: "VS Code Settings", Category: "editor", Kind: File, BackupDest: "editor/vscode/settings.json", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Application Support/Code/User/settings.json", "linux": "~/.config/Code/User/settings.json", "windows": "%APPDATA%/Code/User/settings.json"}},
	{ID: "editor.vscode.keybindings", Name: "VS Code Keybindings", Category: "editor", Kind: File, BackupDest: "editor/vscode/keybindings.json", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Application Support/Code/User/keybindings.json", "linux": "~/.config/Code/User/keybindings.json", "windows": "%APPDATA%/Code/User/keybindings.json"}},
	{ID: "editor.vscode.snippets", Name: "VS Code Snippets", Category: "editor", Kind: Dir, BackupDest: "editor/vscode/snippets", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Application Support/Code/User/snippets", "linux": "~/.config/Code/User/snippets", "windows": "%APPDATA%/Code/User/snippets"}},
	{ID: "editor.helix", Name: "Helix Config", Category: "editor", Kind: Dir, BackupDest: "editor/helix", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/helix", "linux": "~/.config/helix"}},
	{ID: "editor.doom", Name: "Doom Emacs", Category: "editor", Kind: Dir, BackupDest: "editor/doom", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/doom", "linux": "~/.config/doom"}},
	{ID: "editor.sublime", Name: "Sublime Text User", Category: "editor", Kind: Dir, BackupDest: "editor/sublime", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Application Support/Sublime Text/Packages/User", "linux": "~/.config/sublime-text/Packages/User", "windows": "%APPDATA%/Sublime Text/Packages/User"}},
	{ID: "editor.editorconfig", Name: ".editorconfig", Category: "editor", Kind: File, BackupDest: "editor/.editorconfig", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.editorconfig", "linux": "~/.editorconfig", "windows": "%USERPROFILE%/.editorconfig"}},

	// Terminals & prompt
	{ID: "terminal.starship", Name: "Starship prompt", Category: "terminal", Kind: File, BackupDest: "terminal/starship.toml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/starship.toml", "linux": "~/.config/starship.toml", "windows": "%USERPROFILE%/.config/starship.toml"}},
	{ID: "terminal.alacritty", Name: "Alacritty", Category: "terminal", Kind: Dir, BackupDest: "terminal/alacritty", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/alacritty", "linux": "~/.config/alacritty", "windows": "%APPDATA%/alacritty"}},
	{ID: "terminal.kitty", Name: "Kitty", Category: "terminal", Kind: Dir, BackupDest: "terminal/kitty", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/kitty", "linux": "~/.config/kitty"}},
	{ID: "terminal.wezterm", Name: "WezTerm", Category: "terminal", Kind: File, BackupDest: "terminal/wezterm.lua", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/wezterm/wezterm.lua", "linux": "~/.config/wezterm/wezterm.lua"}},
	{ID: "terminal.ghostty", Name: "Ghostty", Category: "terminal", Kind: File, BackupDest: "terminal/ghostty/config", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/ghostty/config", "linux": "~/.config/ghostty/config"}},

	// Shells (more) + GNU/POSIX dotfiles
	{ID: "shell.profile", Name: ".profile", Category: "shell", Kind: File, BackupDest: "shell/.profile", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.profile", "linux": "~/.profile"}},
	{ID: "shell.fish", Name: "Fish Config", Category: "shell", Kind: Dir, BackupDest: "shell/fish", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/fish", "linux": "~/.config/fish"}},
	{ID: "shell.nushell", Name: "Nushell Config", Category: "shell", Kind: Dir, BackupDest: "shell/nushell", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Application Support/nushell", "linux": "~/.config/nushell"}},
	{ID: "shell.inputrc", Name: ".inputrc", Category: "shell", Kind: File, BackupDest: "shell/.inputrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.inputrc", "linux": "~/.inputrc"}},

	// Networking / git extras
	// Scheduled work: launchd agents are how a Mac runs things in the
	// background, and nothing reminds you they exist until they stop.
	{ID: "schedule.launchagents", Name: "launchd agents", Category: "schedule", Kind: Dir, BackupDest: "schedule/LaunchAgents", Sensitivity: Medium,
		Paths: map[string]string{"darwin": "~/Library/LaunchAgents"}},

	{ID: "net.curlrc", Name: ".curlrc", Category: "net", Kind: File, BackupDest: "net/.curlrc", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/.curlrc", "linux": "~/.curlrc"}},
	{ID: "net.wgetrc", Name: ".wgetrc", Category: "net", Kind: File, BackupDest: "net/.wgetrc", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/.wgetrc", "linux": "~/.wgetrc"}},
	{ID: "git.attributes", Name: ".gitattributes_global", Category: "git", Kind: File, BackupDest: "git/.gitattributes_global", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.gitattributes_global", "linux": "~/.gitattributes_global", "windows": "%USERPROFILE%/.gitattributes_global"}},

	// Developer tooling
	{ID: "dev.direnv", Name: "direnv", Category: "dev", Kind: Dir, BackupDest: "dev/direnv", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/direnv", "linux": "~/.config/direnv"}},
	{ID: "apps.karabiner", Name: "Karabiner", Category: "apps", Kind: Dir, BackupDest: "apps/karabiner", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/karabiner"}, Exclude: []string{"automatic_backups"}},
	{ID: "apps.hammerspoon", Name: "Hammerspoon", Category: "apps", Kind: Dir, BackupDest: "apps/hammerspoon", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.hammerspoon"}, Exclude: []string{"Spoons/*.spoon/.git"}},
	{ID: "apps.aerospace", Name: "AeroSpace", Category: "apps", Kind: File, BackupDest: "apps/aerospace.toml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.aerospace.toml"}},
	{ID: "apps.aerospace.xdg", Name: "AeroSpace (XDG)", Category: "apps", Kind: Dir, BackupDest: "apps/aerospace", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/aerospace"}},
	{ID: "apps.yabai", Name: "yabai", Category: "apps", Kind: File, BackupDest: "apps/.yabairc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.yabairc"}},
	{ID: "apps.skhd", Name: "skhd", Category: "apps", Kind: File, BackupDest: "apps/.skhdrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.skhdrc"}},
	{ID: "apps.sketchybar", Name: "SketchyBar", Category: "apps", Kind: Dir, BackupDest: "apps/sketchybar", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/sketchybar"}},
	{ID: "apps.bat", Name: "bat", Category: "apps", Kind: Dir, BackupDest: "apps/bat", Sensitivity: Low, Paths: unix("~/.config/bat")},
	{ID: "apps.ripgrep", Name: ".ripgreprc", Category: "apps", Kind: File, BackupDest: "apps/.ripgreprc", Sensitivity: Low, Paths: unix("~/.ripgreprc")},
	{ID: "apps.atuin", Name: "atuin", Category: "apps", Kind: File, BackupDest: "apps/atuin/config.toml", Sensitivity: Low, Paths: unix("~/.config/atuin/config.toml")},
	{ID: "apps.yazi", Name: "yazi", Category: "apps", Kind: Dir, BackupDest: "apps/yazi", Sensitivity: Low, Paths: unix("~/.config/yazi")},
	{ID: "apps.btop", Name: "btop", Category: "apps", Kind: File, BackupDest: "apps/btop/btop.conf", Sensitivity: Low, Paths: unix("~/.config/btop/btop.conf")},

	// Version managers (declarative config; live installed versions via collectors)
	{ID: "vm.tool-versions", Name: ".tool-versions", Category: "vm", Kind: File, BackupDest: "vm/.tool-versions", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.tool-versions", "linux": "~/.tool-versions"}},
	{ID: "vm.nvmrc", Name: ".nvmrc", Category: "vm", Kind: File, BackupDest: "vm/.nvmrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.nvmrc", "linux": "~/.nvmrc"}},
	{ID: "vm.mise", Name: "mise config", Category: "vm", Kind: File, BackupDest: "vm/mise/config.toml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/mise/config.toml", "linux": "~/.config/mise/config.toml"}},
	{ID: "vm.asdfrc", Name: ".asdfrc", Category: "vm", Kind: File, BackupDest: "vm/.asdfrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.asdfrc", "linux": "~/.asdfrc"}},

	// Secrets / bare credential stores (High — never plaintext)
	{ID: "secrets.netrc", Name: ".netrc", Category: "secrets", Kind: File, BackupDest: "secrets/.netrc", Sensitivity: High, Paths: map[string]string{"darwin": "~/.netrc", "linux": "~/.netrc", "windows": "%USERPROFILE%/_netrc"}},
	{ID: "secrets.vault", Name: "Vault token", Category: "secrets", Kind: File, BackupDest: "secrets/.vault-token", Sensitivity: High, Paths: map[string]string{"darwin": "~/.vault-token", "linux": "~/.vault-token"}},

	// Secrets (carried encrypted) — declarative: a no-op until ~/.gnupg has real keys.
	{ID: "secrets.gnupg", Name: "GnuPG home", Category: "secrets", Kind: Dir, BackupDest: "secrets/gnupg", Sensitivity: High, Paths: map[string]string{"darwin": "~/.gnupg", "linux": "~/.gnupg"}, Exclude: []string{"S.*", "*.lock", ".#*", "random_seed"}},

	// Language & toolchain config (lang). Files that hold tokens by design are
	// High (encrypted on export, excluded from a plaintext backup).
	{ID: "lang.gemrc", Name: ".gemrc", Category: "lang", Kind: File, BackupDest: "lang/ruby/.gemrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.gemrc", "linux": "~/.gemrc"}},
	{ID: "lang.bundle", Name: "Bundler config", Category: "lang", Kind: File, BackupDest: "lang/ruby/bundle-config", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/.bundle/config", "linux": "~/.bundle/config"}},
	{ID: "lang.irbrc", Name: ".irbrc", Category: "lang", Kind: File, BackupDest: "lang/ruby/.irbrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.irbrc", "linux": "~/.irbrc"}},
	{ID: "lang.pip", Name: "pip config", Category: "lang", Kind: File, BackupDest: "lang/python/pip.conf", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/Library/Application Support/pip/pip.conf", "linux": "~/.config/pip/pip.conf"}},
	{ID: "lang.condarc", Name: ".condarc", Category: "lang", Kind: File, BackupDest: "lang/python/.condarc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.condarc", "linux": "~/.condarc"}},
	{ID: "lang.poetry.auth", Name: "Poetry auth", Category: "lang", Kind: File, BackupDest: "lang/python/poetry-auth.toml", Sensitivity: High, Paths: map[string]string{"darwin": "~/Library/Application Support/pypoetry/auth.toml", "linux": "~/.config/pypoetry/auth.toml"}},
	{ID: "lang.go.env", Name: "go env", Category: "lang", Kind: File, BackupDest: "lang/go/env", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/go/env", "linux": "~/.config/go/env"}},
	{ID: "lang.cargo.config", Name: "Cargo config", Category: "lang", Kind: File, BackupDest: "lang/rust/config.toml", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/.cargo/config.toml", "linux": "~/.cargo/config.toml"}},
	{ID: "lang.cargo.credentials", Name: "Cargo credentials", Category: "lang", Kind: File, BackupDest: "lang/rust/credentials.toml", Sensitivity: High, Paths: map[string]string{"darwin": "~/.cargo/credentials.toml", "linux": "~/.cargo/credentials.toml"}},
	{ID: "lang.composer", Name: "Composer config", Category: "lang", Kind: File, BackupDest: "lang/php/composer.json", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.composer/composer.json", "linux": "~/.config/composer/composer.json"}},
	{ID: "lang.composer.auth", Name: "Composer auth", Category: "lang", Kind: File, BackupDest: "lang/php/auth.json", Sensitivity: High, Paths: map[string]string{"darwin": "~/.composer/auth.json", "linux": "~/.config/composer/auth.json"}},
	{ID: "lang.nuget", Name: "NuGet config", Category: "lang", Kind: File, BackupDest: "lang/dotnet/NuGet.Config", Sensitivity: High, Paths: map[string]string{"darwin": "~/.nuget/NuGet/NuGet.Config", "linux": "~/.nuget/NuGet/NuGet.Config", "windows": "%APPDATA%/NuGet/NuGet.Config"}},
	{ID: "lang.yarnrc", Name: ".yarnrc.yml", Category: "lang", Kind: File, BackupDest: "lang/js/.yarnrc.yml", Sensitivity: High, Paths: map[string]string{"darwin": "~/.yarnrc.yml", "linux": "~/.yarnrc.yml"}},
	{ID: "lang.iex", Name: ".iex.exs", Category: "lang", Kind: File, BackupDest: "lang/elixir/.iex.exs", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.iex.exs", "linux": "~/.iex.exs"}},
	{ID: "lang.julia", Name: "Julia startup", Category: "lang", Kind: File, BackupDest: "lang/julia/startup.jl", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.julia/config/startup.jl", "linux": "~/.julia/config/startup.jl"}},

	// More cloud / devops / git-CLI coverage. Credential-bearing entries are High.
	{ID: "git.gh.hosts", Name: "GitHub CLI hosts", Category: "git", Kind: File, BackupDest: "git/gh/hosts.yml", Sensitivity: High, Paths: map[string]string{"darwin": "~/.config/gh/hosts.yml", "linux": "~/.config/gh/hosts.yml", "windows": "%APPDATA%/GitHub CLI/hosts.yml"}},
	{ID: "git.glab", Name: "GitLab CLI", Category: "git", Kind: File, BackupDest: "git/glab/config.yml", Sensitivity: High, Paths: map[string]string{"darwin": "~/.config/glab-cli/config.yml", "linux": "~/.config/glab-cli/config.yml"}},
	{ID: "cloud.teleport", Name: "Teleport (tsh)", Category: "cloud", Kind: Dir, BackupDest: "cloud/tsh", Sensitivity: High, Paths: map[string]string{"darwin": "~/.tsh", "linux": "~/.tsh"}},
	{ID: "cloud.ngrok", Name: "ngrok", Category: "cloud", Kind: File, BackupDest: "cloud/ngrok/ngrok.yml", Sensitivity: High, Paths: map[string]string{"darwin": "~/Library/Application Support/ngrok/ngrok.yml", "linux": "~/.config/ngrok/ngrok.yml"}},
	{ID: "cloud.scaleway", Name: "Scaleway CLI", Category: "cloud", Kind: File, BackupDest: "cloud/scw/config.yaml", Sensitivity: High, Paths: map[string]string{"darwin": "~/.config/scw/config.yaml", "linux": "~/.config/scw/config.yaml"}},
	{ID: "cloud.argocd", Name: "Argo CD", Category: "cloud", Kind: File, BackupDest: "cloud/argocd/config", Sensitivity: High, Paths: map[string]string{"darwin": "~/.config/argocd/config", "linux": "~/.config/argocd/config"}},
	{ID: "cloud.onepassword", Name: "1Password CLI", Category: "cloud", Kind: File, BackupDest: "cloud/op/config.json", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/.config/op/config", "linux": "~/.config/op/config"}},
	{ID: "devops.ansible", Name: "Ansible config", Category: "devops", Kind: File, BackupDest: "devops/ansible.cfg", Sensitivity: Medium, Paths: map[string]string{"darwin": "~/.ansible.cfg", "linux": "~/.ansible.cfg"}},
	{ID: "devops.terraformrc", Name: "Terraform CLI config", Category: "devops", Kind: File, BackupDest: "devops/terraform/.terraformrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.terraformrc", "linux": "~/.terraformrc"}},
	{ID: "devops.packer", Name: "Packer config", Category: "devops", Kind: File, BackupDest: "devops/.packerconfig", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.packerconfig", "linux": "~/.packerconfig"}},
	{ID: "devops.skaffold", Name: "Skaffold config", Category: "devops", Kind: File, BackupDest: "devops/skaffold/config", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.skaffold/config", "linux": "~/.skaffold/config"}},

	// Git ecosystem: XDG config, SSH-signing allowlist, global hooks, companions.
	{ID: "git.xdg.config", Name: "XDG git config", Category: "git", Kind: File, BackupDest: "git/config", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/git/config", "linux": "~/.config/git/config"}},
	{ID: "git.xdg.ignore", Name: "XDG git ignore", Category: "git", Kind: File, BackupDest: "git/ignore", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/git/ignore", "linux": "~/.config/git/ignore"}},
	{ID: "git.xdg.attributes", Name: "XDG git attributes", Category: "git", Kind: File, BackupDest: "git/attributes", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/git/attributes", "linux": "~/.config/git/attributes"}},
	{ID: "git.allowed_signers", Name: "git allowed_signers", Category: "git", Kind: File, BackupDest: "git/allowed_signers", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/git/allowed_signers", "linux": "~/.config/git/allowed_signers"}},
	{ID: "git.hooks", Name: "Global git hooks", Category: "git", Kind: Dir, BackupDest: "git/hooks", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/git/hooks", "linux": "~/.config/git/hooks"}},
	{ID: "git.lazygit", Name: "lazygit", Category: "git", Kind: File, BackupDest: "git/lazygit/config.yml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Application Support/lazygit/config.yml", "linux": "~/.config/lazygit/config.yml"}},
	{ID: "git.jj", Name: "Jujutsu config", Category: "git", Kind: File, BackupDest: "git/jj/config.toml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/jj/config.toml", "linux": "~/.config/jj/config.toml"}},
	{ID: "git.gitui", Name: "gitui", Category: "git", Kind: Dir, BackupDest: "git/gitui", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/gitui", "linux": "~/.config/gitui"}},
	{ID: "git.tig", Name: ".tigrc", Category: "git", Kind: File, BackupDest: "git/.tigrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.tigrc", "linux": "~/.tigrc"}},

	// Database clients.
	{ID: "db.pg_service", Name: ".pg_service.conf", Category: "db", Kind: File, BackupDest: "db/.pg_service.conf", Sensitivity: High, Paths: map[string]string{"darwin": "~/.pg_service.conf", "linux": "~/.pg_service.conf"}},
	{ID: "db.pgcli", Name: "pgcli config", Category: "db", Kind: File, BackupDest: "db/pgcli/config", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/pgcli/config", "linux": "~/.config/pgcli/config"}},
	{ID: "db.mycli", Name: ".myclirc", Category: "db", Kind: File, BackupDest: "db/.myclirc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.myclirc", "linux": "~/.myclirc"}},
	{ID: "db.litecli", Name: "litecli config", Category: "db", Kind: File, BackupDest: "db/litecli/config", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/litecli/config", "linux": "~/.config/litecli/config"}},
	{ID: "db.mongosh", Name: ".mongoshrc.js", Category: "db", Kind: File, BackupDest: "db/.mongoshrc.js", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.mongoshrc.js", "linux": "~/.mongoshrc.js"}},

	// Editors (more) — IdeaVim (the portable JetBrains config; full IDE settings
	// are version/product-specific and out of scope here).
	{ID: "editor.ideavim", Name: ".ideavimrc", Category: "editor", Kind: File, BackupDest: "editor/.ideavimrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.ideavimrc", "linux": "~/.ideavimrc", "windows": "%USERPROFILE%/.ideavimrc"}},

	// Shell frameworks & plugin manifests.
	{ID: "shell.ohmyzsh.custom", Name: "oh-my-zsh custom", Category: "shell", Kind: Dir, BackupDest: "shell/oh-my-zsh-custom", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.oh-my-zsh/custom", "linux": "~/.oh-my-zsh/custom"}},
	{ID: "shell.sheldon", Name: "sheldon plugins", Category: "shell", Kind: File, BackupDest: "shell/sheldon/plugins.toml", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/sheldon/plugins.toml", "linux": "~/.config/sheldon/plugins.toml"}},
	{ID: "shell.antidote", Name: "antidote plugins", Category: "shell", Kind: File, BackupDest: "shell/.zsh_plugins.txt", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.zsh_plugins.txt", "linux": "~/.zsh_plugins.txt"}},
	{ID: "shell.powershell", Name: "PowerShell profile", Category: "shell", Kind: File, BackupDest: "shell/powershell/profile.ps1", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/powershell/Microsoft.PowerShell_profile.ps1", "linux": "~/.config/powershell/Microsoft.PowerShell_profile.ps1"}},

	// Terminals & multiplexers (more).
	{ID: "terminal.zellij", Name: "Zellij", Category: "terminal", Kind: Dir, BackupDest: "terminal/zellij", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.config/zellij", "linux": "~/.config/zellij"}},
	{ID: "terminal.screen", Name: ".screenrc", Category: "terminal", Kind: File, BackupDest: "terminal/.screenrc", Sensitivity: Low, Paths: map[string]string{"darwin": "~/.screenrc", "linux": "~/.screenrc"}},

	// Mobile. The Android debug keystore signs every debug build; its SHA-1 is
	// registered with Google Sign-In, Maps and Firebase, so a new one breaks
	// them until every console is updated.
	{ID: "mobile.android.debugkey", Name: "Android debug keystore", Category: "mobile", Kind: File, BackupDest: "mobile/android/debug.keystore", Sensitivity: High, Paths: unix("~/.android/debug.keystore")},
	{ID: "mobile.xcode.keybindings", Name: "Xcode key bindings", Category: "mobile", Kind: Dir, BackupDest: "mobile/xcode/KeyBindings", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Developer/Xcode/UserData/KeyBindings"}},
	{ID: "mobile.xcode.themes", Name: "Xcode themes", Category: "mobile", Kind: Dir, BackupDest: "mobile/xcode/FontAndColorThemes", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Developer/Xcode/UserData/FontAndColorThemes"}},
	{ID: "mobile.xcode.snippets", Name: "Xcode code snippets", Category: "mobile", Kind: Dir, BackupDest: "mobile/xcode/CodeSnippets", Sensitivity: Low, Paths: map[string]string{"darwin": "~/Library/Developer/Xcode/UserData/CodeSnippets"}},

	// dothaven's own list of extra paths, so it survives a restore.
	{ID: "dothaven.include", Name: "dothaven include list", Category: "dothaven", Kind: File, BackupDest: "dothaven/include", Sensitivity: Low, Paths: unix("~/.config/dothaven/include")},
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

// Collect reads every entry that exists on disk into a snapshot. It checks ctx
// between entries so a cancelled run (Ctrl-C) stops the file-read pass promptly
// rather than reading every remaining source.
func Collect(ctx context.Context, env sys.Env, home string, redact bool, entries []Entry) snapshot.Snapshot {
	out := snapshot.Snapshot{}
	for _, e := range entries {
		if ctx.Err() != nil {
			break
		}
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
			content := string(b)
			lines := strings.Count(content, "\n")
			if len(content) > 0 && !strings.HasSuffix(content, "\n") {
				lines++ // a final line with no trailing newline still counts
			}
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
		sort.Strings(keys) // empty Fields = all keys; sort so output is deterministic
	}
	pairs := map[string]string{}
	for _, f := range keys {
		v, ok := data[f]
		if !ok {
			continue
		}
		// Flatten one level, namespacing children as parent.child. A bare child
		// key would let two sibling objects (or a scalar) collide, and the winner
		// was decided by random map-iteration order — breaking the deterministic
		// snapshot guarantee. Namespacing makes it both collision-free and stable.
		if obj, ok := v.(map[string]any); ok {
			for _, k := range sortedAnyKeys(obj) {
				pairs[f+"."+k] = jsString(obj[k])
			}
		} else {
			pairs[f] = jsString(v)
		}
	}
	return pairs
}

func sortedAnyKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
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
