#!/bin/bash
# DEPRECATED: Use `bun bin/dotfiles.ts collect` instead.
# This script outputs markdown. The new CLI outputs .json format with
# structured sections, redaction, comparison, and section listing.
#
# Collects AI tool configs and system info from any machine.
# Run on work Mac, then copy the output file back to compare.
#
# Usage:
#   bash collect-machine-config.sh           # outputs to ./reports/ if in repo, else ~/
#   bash collect-machine-config.sh -o /path  # custom output directory

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
HOSTNAME="$(hostname -s)"
FILENAME="dotfiles-report-${HOSTNAME}.md"

OUTPUT_DIR=""
while getopts "o:" opt; do
  case $opt in
    o) OUTPUT_DIR="$OPTARG" ;;
    *) ;;
  esac
done

if [ -n "$OUTPUT_DIR" ]; then
  mkdir -p "$OUTPUT_DIR"
elif [ -d "$REPO_ROOT/reports" ] || [ -d "$REPO_ROOT/.git" ]; then
  OUTPUT_DIR="$REPO_ROOT/reports"
  mkdir -p "$OUTPUT_DIR"
else
  OUTPUT_DIR="$HOME"
fi

REPORT="$OUTPUT_DIR/$FILENAME"

{
  echo "# Machine Config Report"
  echo "- **Host:** $HOSTNAME"
  echo "- **OS:** $(uname -s) $(uname -m)"
  echo "- **Date:** $(date +%Y-%m-%d)"
  echo ""

  echo "## Claude Code"
  echo "### settings.json"
  echo '```json'
  cat ~/.claude/settings.json 2>/dev/null || echo "(not found)"
  echo '```'
  echo "### CLAUDE.md"
  echo '```markdown'
  cat ~/.claude/CLAUDE.md 2>/dev/null || echo "(not found)"
  echo '```'
  echo ""

  echo "## Cursor"
  echo "### mcp.json"
  echo '```json'
  cat ~/.cursor/mcp.json 2>/dev/null || echo "(not found)"
  echo '```'
  echo "### Skills"
  echo '```'
  ls ~/.cursor/skills/ 2>/dev/null || echo "(not found)"
  echo '```'
  echo ""

  echo "## Gemini CLI"
  echo "### settings.json"
  echo '```json'
  cat ~/.gemini/settings.json 2>/dev/null || echo "(not found)"
  echo '```'
  echo "### GEMINI.md"
  echo '```markdown'
  cat ~/.gemini/GEMINI.md 2>/dev/null || echo "(not found)"
  echo '```'
  echo "### Skills"
  echo '```'
  ls ~/.gemini/skills/ 2>/dev/null || echo "(not found)"
  echo '```'
  echo ""

  echo "## Windsurf"
  echo "### mcp_config.json"
  echo '```json'
  cat ~/.codeium/windsurf/mcp_config.json 2>/dev/null || echo "(not found)"
  echo '```'
  echo "### Skills"
  echo '```'
  ls ~/.codeium/windsurf/skills/ 2>/dev/null || echo "(not found)"
  echo '```'
  echo ""

  echo "## Shell"
  echo "### .zshrc"
  echo '```bash'
  cat ~/.zshrc 2>/dev/null || echo "(not found)"
  echo '```'
  echo ""

  echo "## Git"
  echo "### .gitconfig"
  echo '```ini'
  cat ~/.gitconfig 2>/dev/null || echo "(not found)"
  echo '```'
  echo ""

  echo "## Editors"
  echo "### Zed settings.json"
  echo '```json'
  cat ~/.config/zed/settings.json 2>/dev/null || echo "(not found)"
  echo '```'
  echo "### Cursor editor settings"
  echo '```json'
  cat ~/Library/Application\ Support/Cursor/User/settings.json 2>/dev/null || echo "(not found)"
  echo '```'
  echo ""

  echo "## Terminal"
  echo "### p10k theme"
  echo '```'
  [ -f ~/.p10k.zsh ] && echo "(exists, $(wc -l < ~/.p10k.zsh) lines)" || echo "(not found)"
  echo '```'
  echo ""

  echo "## SSH Config"
  echo '```'
  if [ -f ~/.ssh/config ]; then
    # Redact IP addresses and key paths for safety
    sed 's/HostName .*/HostName [REDACTED]/; s/IdentityFile .*/IdentityFile [REDACTED]/' ~/.ssh/config
  else
    echo "(not found)"
  fi
  echo '```'
  echo ""

  echo "## GitHub CLI"
  echo '```yaml'
  cat ~/.config/gh/config.yml 2>/dev/null || echo "(not found)"
  echo '```'
  echo ""

  echo "## npm config (.npmrc)"
  echo '```'
  if [ -f ~/.npmrc ]; then
    # Redact auth tokens
    sed 's/_authToken=.*/_authToken=[REDACTED]/' ~/.npmrc
  else
    echo "(not found)"
  fi
  echo '```'
  echo ""

  echo "## Bun config (bunfig.toml)"
  echo '```toml'
  cat ~/.bunfig.toml 2>/dev/null || echo "(not found)"
  echo '```'
  echo ""

  echo "## Raycast"
  echo '```'
  [ -d "/Applications/Raycast.app" ] && echo "(installed)" || echo "(not installed)"
  echo '```'
  echo ""

  echo "## AltTab"
  echo '```'
  if [ -d "/Applications/AltTab.app" ]; then
    echo "(installed)"
    defaults read com.lwouis.alt-tab-macos 2>/dev/null | head -30 || echo "(no preferences found)"
  else
    echo "(not installed)"
  fi
  echo '```'
  echo ""

  echo "## macOS Apps (/Applications)"
  echo '```'
  ls /Applications/ 2>/dev/null | sort || echo "(unable to list)"
  echo '```'
  echo ""

  echo "## Homebrew"
  echo '```'
  brew list --formula 2>/dev/null | sort || echo "(brew not found)"
  echo '```'
  echo "### Casks"
  echo '```'
  brew list --cask 2>/dev/null | sort || echo "(brew not found)"
  echo '```'

} > "$REPORT"

echo "Report saved to: $REPORT"
