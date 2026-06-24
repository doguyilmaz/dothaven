package chezmoi

import (
	"strings"

	"github.com/doguyilmaz/dothaven/internal/registry"
)

// templatableCategories are config categories that commonly embed an absolute
// home-directory path (shell rc files exporting PATH, gitconfig include paths,
// prompt/editor config) and so benefit from being added as chezmoi templates.
var templatableCategories = map[string]bool{
	"shell":    true,
	"git":      true,
	"terminal": true,
	"editor":   true,
	"dev":      true,
	"vm":       true,
}

// ShouldTemplate reports whether a config entry is worth adding as a chezmoi
// template: a plain (non-secret) text file in a category that commonly hard-codes
// the home path. Dir entries are excluded (templating a whole tree is a separate
// concern) and secrets are encrypted rather than templated, so the caller only
// asks this for entries it has already decided not to encrypt.
func ShouldTemplate(e registry.Entry) bool {
	return e.Kind == registry.File && templatableCategories[e.Category]
}

// HomeDirVar is the chezmoi template expression for the target's home directory.
const HomeDirVar = "{{ .chezmoi.homeDir }}"

// Templatize rewrites a file's content for portability across machines by
// replacing the absolute home-directory prefix with chezmoi's homeDir template
// variable. It is deliberately conservative — only `<home>/` is rewritten, so a
// sibling like /Users/devops is never corrupted when home is /Users/dev, and no
// greedy username/value substitution (autotemplate's known footgun) happens.
// Returns the (possibly unchanged) content and whether anything was rewritten.
func Templatize(content, home string) (string, bool) {
	if len(home) < 2 { // guard against "" and "/"
		return content, false
	}
	out := strings.ReplaceAll(content, home+"/", HomeDirVar+"/")
	return out, out != content
}
