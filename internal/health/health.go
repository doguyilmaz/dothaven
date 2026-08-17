// Package health checks whether config files still parse.
//
// A broken config usually fails quietly: a shell that skips the rest of your
// .zshrc after a syntax error, a JSON settings file an editor silently ignores.
// You notice weeks later, and by then it has been copied into every backup and
// carried to every machine. Checking is cheap; finding out later is not.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Status is the outcome for one file.
type Status int

const (
	OK        Status = iota
	Broken           // the format's own parser rejected it
	Unchecked        // no parser available for this format on this machine
)

// Result is one file's verdict.
type Result struct {
	Path   string
	Format string
	Status Status
	Detail string // the parser's complaint, trimmed to something readable
}

// Runner executes a validator. Injected for testing.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// ExecRunner runs a real command and returns its combined output, which is
// where parsers put their complaints.
func ExecRunner(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// Check validates one file using the parser that owns its format.
//
// Deliberately no third-party YAML or TOML libraries: a config is correct when
// the program that reads it accepts it, so asking that program is both more
// accurate than a re-implementation and nothing extra to trust. Formats with no
// parser to hand are reported Unchecked rather than assumed fine — a checker
// that quietly skips things is worse than one that admits its limits.
func Check(ctx context.Context, run Runner, path string) Result {
	r := Result{Path: path, Format: formatOf(path)}

	switch r.Format {
	case "json":
		data, err := os.ReadFile(path)
		if err != nil {
			return Result{Path: path, Format: r.Format, Status: Unchecked, Detail: "unreadable"}
		}
		// Editors write JSON with comments (jsonc) in settings files; that is
		// valid for them and invalid for encoding/json, so it is not a fault.
		if looksLikeJSONC(data) {
			r.Status = Unchecked
			r.Detail = "contains comments (jsonc)"
			return r
		}
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			r.Status, r.Detail = Broken, trimDetail(err.Error())
			return r
		}
	case "shell":
		shell := "bash"
		if strings.Contains(filepath.Base(path), "zsh") {
			shell = "zsh"
		}
		out, err := run(ctx, shell, "-n", path)
		return verdict(r, out, err)
	case "git":
		out, err := run(ctx, "git", "config", "--file", path, "--list")
		return verdict(r, out, err)
	case "ssh":
		// -G resolves the config for a host without connecting, which parses
		// the whole file and reports the first syntax error.
		out, err := run(ctx, "ssh", "-F", path, "-G", "dothaven-syntax-check")
		return verdict(r, out, err)
	default:
		r.Status = Unchecked
		r.Detail = "no parser for this format"
	}
	return r
}

// verdict turns a validator's outcome into a status, separating "this file is
// wrong" from "this machine cannot tell".
//
// A missing validator is not a broken file. A Linux box without zsh installed
// would otherwise report every .zshrc as broken — a health check that invents
// faults is worse than one that admits it cannot see, because the first thing
// it costs you is the habit of reading it.
func verdict(r Result, out string, err error) Result {
	if err == nil {
		return r
	}
	if errors.Is(err, exec.ErrNotFound) {
		r.Status = Unchecked
		r.Detail = "no parser for this format on this machine"
		return r
	}
	r.Status, r.Detail = Broken, trimDetail(out)
	return r
}

// formatOf maps a path to the parser that owns it.
func formatOf(path string) string {
	base := filepath.Base(path)
	switch {
	case strings.HasSuffix(base, ".json"):
		return "json"
	case base == ".gitconfig" || base == "config" && strings.Contains(path, "/git/"):
		return "git"
	case base == "config" && strings.Contains(path, "/.ssh/"):
		return "ssh"
	case strings.HasSuffix(base, ".zsh"), strings.HasSuffix(base, ".bash"),
		strings.HasSuffix(base, ".sh"), base == ".zshrc", base == ".zprofile",
		base == ".zshenv", base == ".bashrc", base == ".bash_profile",
		base == ".profile", base == ".zlogin", base == ".bash_login":
		return "shell"
	}
	return ""
}

// looksLikeJSONC reports whether the file uses comments, which several editors
// allow in their settings and encoding/json does not.
func looksLikeJSONC(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") {
			return true
		}
	}
	return false
}

// trimDetail reduces a parser's output to its first meaningful line.
func trimDetail(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if len(line) > 120 {
				line = line[:117] + "…"
			}
			return line
		}
	}
	return "failed to parse"
}
