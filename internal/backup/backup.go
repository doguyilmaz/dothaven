// Package backup copies tracked config files into a timestamped backup tree,
// applying the same redaction/skip gate as collect so a plaintext backup never
// carries a raw secret.
package backup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
	"github.com/doguyilmaz/dothaven/internal/sys"
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
	// SkippedSensitive lists dests excluded because they are high-sensitivity
	// with no guaranteed redactor — they belong in the encrypted export, not a
	// plaintext backup.
	SkippedSensitive []string
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
		// A plaintext backup must never hold an unredactable secret. A
		// high-sensitivity entry with no guaranteed redactor (e.g. ~/.gnupg,
		// cloud credentials) is excluded from a redacting backup — content
		// scanning is best-effort and misses binary key material. The encrypted
		// `chezmoi-export` path handles these instead.
		if opts.Redact && t.Sensitivity == registry.High && t.Redact == nil {
			if _, err := os.Stat(t.Src); err == nil {
				res.SkippedSensitive = append(res.SkippedSensitive, t.Dest)
			}
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

// maxBackupFileSize caps a single file copied into a backup. Config files are
// small; a larger file is anomalous and is skipped rather than read whole into
// memory (and, unscanned, it could smuggle a secret into a plaintext backup).
const maxBackupFileSize = 16 << 20 // 16 MiB

func tooLarge(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > maxBackupFileSize
}

func copyFile(t registry.BackupTarget, destRoot string, redact bool, results *[]scan.Result) (int, error) {
	if tooLarge(t.Src) {
		return 0, nil
	}
	raw, err := os.ReadFile(t.Src)
	if err != nil {
		return 0, nil // missing/unreadable → skip silently
	}
	body, keep := gate(t.Dest, string(raw), redact, t.Redact, results)
	if !keep {
		return 0, nil
	}
	if err := sys.WriteFileSecure(filepath.Join(destRoot, t.Dest), body); err != nil {
		return 0, err
	}
	return 1, nil
}

// ManifestMeta is the run context recorded in a backup's MANIFEST.
type ManifestMeta struct {
	Host     string
	OS       string
	Version  string
	Created  string // pre-formatted timestamp
	Redacted bool
}

// Manifest renders a self-describing MANIFEST for a backup tree: what was
// captured, what was deliberately excluded, and how to restore it. A backup you
// can't audit for completeness is dangerous — the exclusion list is the
// safety-critical part, so it travels inside the backup rather than scrolling
// past once in the console.
func Manifest(meta ManifestMeta, res Result) string {
	var b strings.Builder
	b.WriteString("# dothaven backup\n#\n")
	fmt.Fprintf(&b, "# host:     %s\n", meta.Host)
	fmt.Fprintf(&b, "# os:       %s\n", meta.OS)
	fmt.Fprintf(&b, "# created:  %s\n", meta.Created)
	fmt.Fprintf(&b, "# dothaven: %s\n", meta.Version)
	redacted := "no (raw values kept)"
	if meta.Redacted {
		redacted = "yes (secrets redacted)"
	}
	fmt.Fprintf(&b, "# redacted: %s\n#\n", redacted)
	b.WriteString("# Restore on a new machine with:\n#   dothaven restore <this-directory>\n#\n")

	fmt.Fprintf(&b, "# Captured — %d file(s):\n", res.TotalFiles)
	cats := make([]string, 0, len(res.PerCategory))
	for c := range res.PerCategory {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	for _, c := range cats {
		fmt.Fprintf(&b, "#   %s (%d)\n", c, res.PerCategory[c])
	}
	b.WriteString("#\n")

	if len(res.SkippedSensitive) > 0 {
		b.WriteString("# Excluded from this plaintext backup (high-sensitivity).\n")
		b.WriteString("# Carry these age-encrypted with: dothaven chezmoi-export --apply\n")
		excl := append([]string(nil), res.SkippedSensitive...)
		sort.Strings(excl)
		for _, d := range excl {
			fmt.Fprintf(&b, "#   %s\n", d)
		}
	} else {
		b.WriteString("# Excluded: none.\n")
	}
	return b.String()
}

// copyDir mirrors a directory recursively (dotfiles included). A directory entry
// has no per-entry redact rule — only the scan gate applies.
func copyDir(t registry.BackupTarget, destRoot string, redact bool, results *[]scan.Result) (int, error) {
	count := 0
	walkErr := filepath.WalkDir(t.Src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if tooLarge(path) {
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
		if werr := sys.WriteFileSecure(filepath.Join(destRoot, destRel), body); werr != nil {
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
