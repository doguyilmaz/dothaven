#!/bin/bash
# Quick compare — diff two .json reports or auto-pick latest two
# Usage: bash setup/compare.sh [file1.json] [file2.json]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

if ! command -v bun &>/dev/null; then
    echo "Bun is required. Install: https://bun.sh"
    exit 1
fi

cd "$REPO_ROOT" && bun bin/dotfiles.ts compare "$@"
