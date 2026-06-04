// Package backup copies tracked config files into a timestamped backup tree,
// applying the same redaction/skip gate as collect so a plaintext backup never
// carries a raw secret.
package backup

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
)

// Options configure a backup run.
type Options struct {
	Redact bool
	Only   []string
	Skip   []string
}

// Result summarizes a backup run.
type Result struct {
	TotalFiles  int
	PerCategory map[string]int
	ScanResults []scan.Result
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// selected applies the --only/--skip filters: skip always wins; a non-empty
// only-list restricts to its members.
func selected(category string, only, skip []string) bool {
	if contains(skip, category) {
		return false
	}
	return len(only) == 0 || contains(only, category)
}

// Run copies every selected target into destRoot. Missing or unreadable sources
// are skipped silently (a tool may simply not be installed).
func Run(targets []registry.BackupTarget, destRoot string, opts Options) (Result, error) {
	res := Result{PerCategory: map[string]int{}}
	for _, t := range targets {
		if !selected(t.Category, opts.Only, opts.Skip) {
			continue
		}
		var n int
		var err error
		if t.IsDir {
			n, err = copyDir(t, destRoot, opts.Redact, &res.ScanResults)
		} else {
			n, err = copyFile(t, destRoot, opts.Redact, &res.ScanResults)
		}
		if err != nil {
			return res, err
		}
		if n > 0 {
			res.PerCategory[t.Category] += n
			res.TotalFiles += n
		}
	}
	return res, nil
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// gate applies the redaction/skip decision to one file's content. It returns the
// (possibly scrubbed) content and whether the file should be written at all — a
// skip-action finding (e.g. a private key) is never copied to a plaintext backup.
func gate(scanPath, body string, redact bool, entryRedact func(string) string, results *[]scan.Result) (string, bool) {
	sr := scan.ScanContent(scanPath, body)
	if redact && sr.Action == scan.Skip {
		*results = append(*results, sr)
		return "", false
	}
	if redact && entryRedact != nil {
		body = entryRedact(body)
	}
	if redact {
		body = scan.ApplyRedactions(body, sr)
	}
	*results = append(*results, sr)
	return body, true
}

func copyFile(t registry.BackupTarget, destRoot string, redact bool, results *[]scan.Result) (int, error) {
	raw, err := os.ReadFile(t.Src)
	if err != nil {
		return 0, nil // missing/unreadable → skip silently
	}
	body, keep := gate(t.Dest, string(raw), redact, t.Redact, results)
	if !keep {
		return 0, nil
	}
	if err := writeFile(filepath.Join(destRoot, t.Dest), body); err != nil {
		return 0, err
	}
	return 1, nil
}

// copyDir mirrors a directory recursively (dotfiles included). A directory entry
// has no per-entry redact rule — only the scan gate applies.
func copyDir(t registry.BackupTarget, destRoot string, redact bool, results *[]scan.Result) (int, error) {
	count := 0
	walkErr := filepath.WalkDir(t.Src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		rel, _ := filepath.Rel(t.Src, path)
		destRel := filepath.Join(t.Dest, rel)
		body, keep := gate(destRel, string(raw), redact, nil, results)
		if !keep {
			return nil
		}
		if werr := writeFile(filepath.Join(destRoot, destRel), body); werr != nil {
			return werr
		}
		count++
		return nil
	})
	if walkErr != nil {
		return count, nil // dir doesn't exist / unreadable → skip silently
	}
	return count, nil
}
