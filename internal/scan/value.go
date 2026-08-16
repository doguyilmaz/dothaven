package scan

import "strings"

// valueLooksReal reports whether a keyword rule's match carries an actual
// secret rather than code that happens to mention the word.
//
// The scanner was reporting HIGH findings on a zsh theme: `token ==` (a
// comparison, where the rule read the second `=` as the value) and
// `token=$tokens[1]` (a variable reference). Both are noise, and noise is not
// harmless here — a security tool that flags your shell theme is one you learn
// to skim, which costs more than the findings it invents.
//
// Only keyword rules are filtered. A PEM header or an AWS key's shape means
// what it says.
func valueLooksReal(match string) bool {
	i := strings.IndexAny(match, "=:")
	if i < 0 {
		return true // no delimiter to reason about; leave the match alone
	}
	// Trim any further delimiters too: zsh writes `token::=${x}` and shells
	// write `token ==`, where splitting on the first one alone leaves a value
	// that starts with the rest of the operator rather than with the data.
	v := strings.TrimLeft(match[i+1:], " \t=:")
	// `${VAR:+x}`, `${VAR:-x}`, `${VAR:?x}` — shell parameter expansion, where
	// what follows the delimiter is a fallback rule and not a value at all.
	if len(v) > 1 && (v[0] == '+' || v[0] == '-' || v[0] == '?') {
		v = v[1:]
	}
	v = strings.Trim(v, `"'`)
	if v == "" {
		return false
	}

	switch v[0] {
	case '=', '~', '<', '>', '!':
		// `==`, `=~`, `<=`: the rule's delimiter was half of an operator.
		return false
	case '$', '%', '`':
		// $VAR, ${VAR}, %s, `cmd` — the value lives somewhere else.
		return false
	case '{', '(', '[':
		// A structure, not a scalar: `token: {` opens a JSON/YAML block.
		return false
	}

	// Placeholders are what documentation and sample configs are made of, and
	// flagging them trains the same skimming as flagging code.
	lower := strings.ToLower(strings.Trim(v, `<>[](){},;`))
	switch lower {
	case "null", "nil", "none", "true", "false", "changeme", "example",
		"placeholder", "your_token", "your_key", "your_secret", "todo", "xxx", "...":
		return false
	}
	if strings.HasPrefix(lower, "your_") || strings.HasPrefix(lower, "your-") ||
		strings.HasPrefix(lower, "<") || strings.HasPrefix(lower, "xxxx") {
		return false
	}
	return true
}

// looksBinary reports whether content is binary, judged by a NUL byte in the
// first 8 KiB — the same heuristic git uses.
//
// Compiled zsh (.zwc), object files and images match plenty of rules out of
// arbitrary bytes, none of it meaningful. Binary content is therefore matched
// only against key-material rules (Action == Skip), because GnuPG stores a
// private key as a binary s-expression — skipping such files outright would
// lose the one finding that matters most in them.
func looksBinary(content string) bool {
	if len(content) > 8<<10 {
		content = content[:8<<10]
	}
	return strings.IndexByte(content, 0) >= 0
}
