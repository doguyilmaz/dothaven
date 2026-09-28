// Package backup copies tracked config files into a timestamped backup tree,
// applying the same redaction/skip gate as collect so a plaintext backup never
// carries a raw secret.
package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/registry"
	"github.com/doguyilmaz/dothaven/internal/scan"
)

// Options configure a backup run.
type Options struct {
	// Context, when set, is checked between files so Ctrl-C stops a large
	// backup promptly instead of after the last file.
	Context context.Context
	Redact  bool
	// Encrypted marks a run whose output is encrypted as a whole. Nothing is
	// written in plaintext, so there is no gate to apply and no reason to
	// spend a scan on every file.
	Encrypted bool
	Only      []string
	Skip      []string
	// MaxFileSize overrides the per-file cap (0 → MaxFileSize).
	MaxFileSize int64
	// Remote marks a run whose output leaves this machine (a GitHub push).
	// LocalOnly entries, and any file holding an age identity, stay behind.
	Remote bool
	// SkipVCS leaves .git and similar folders out of the walk, for output
	// that cannot hold them (a readable push).
	SkipVCS bool
	// Reading, when set, is told each entry as its walk starts and each file
	// just before it is read, so a caller can show where a slow backup is and
	// which file a stall is on.
	Reading func(dest string, file bool)
}

// maxGateText is the largest text file the plaintext gate will scan. Scanning
// is linear, but forty patterns over a 60 MiB log is seconds per file, and a
// file that size in a config directory is almost never config. Rather than let
// it through unchecked, a redacting backup leaves it out and says so; the
// encrypted backup carries it.
const maxGateText = 8 << 20

// Result summarizes a backup run.
type Result struct {
	TotalFiles  int
	TotalBytes  int64
	PerCategory map[string]int
	// SourceBytes is how much each entry's folder or file added, by its path
	// on this machine, so a large backup can say what made it large.
	SourceBytes map[string]int64
	ScanResults []scan.Result
	// SkippedSensitive lists dests excluded because they are high-sensitivity
	// with no guaranteed redactor. They belong in an encrypted backup, not a
	// plaintext one.
	SkippedSensitive []string
	// ReadErrors lists dests for sources that exist but could not be read
	// (permission/I-O errors, as opposed to absent). A safety-net backup
	// must surface these rather than silently omit a file the user expects.
	ReadErrors []string
	// TooLarge lists files over the size cap. They exist, the user may well
	// expect them, and a backup that leaves them out without saying so is one
	// that loses them on the day it is needed.
	TooLarge []Skipped
	// Withheld lists files dropped by the redaction gate because they hold a
	// private key. The plaintext backup is right to leave them out; the user
	// still has to be told where they did not go.
	Withheld []string
	// RawSecrets lists dests whose content scanned as skip-action (a private key)
	// but were written verbatim because redaction was off. The CLI warns loudly
	// when that happened in a plaintext tree.
	RawSecrets []string
	// KeptLocal lists files a remote run left on this machine on purpose (see
	// Options.Remote): keys that protect other copies.
	KeptLocal []string
	// Rebuildable lists folders left out because the new machine rebuilds
	// them: dependencies, caches, virtual environments, build output.
	Rebuildable []Skipped
}

// Run copies every selected target into destRoot.
func Run(targets []registry.BackupTarget, destRoot string, opts Options) (Result, error) {
	return RunTo(targets, DirSink{Root: destRoot}, opts)
}

// RunTo copies every selected target into sink. Missing sources are skipped
// silently (a tool may not be installed); everything else that is not
// carried is recorded in the Result.
func RunTo(targets []registry.BackupTarget, sink Sink, opts Options) (Result, error) {
	res := Result{PerCategory: map[string]int{}}
	written := map[string]bool{}    // by dest
	writtenSrc := map[string]bool{} // by source path
	// Credential roots, whichever entry reaches them. A user who includes
	// ~/.aws must not get ~/.aws/credentials in plaintext just because it came
	// in through their own path rather than the registry's.
	var guarded, localOnly []string
	for _, t := range targets {
		if t.Sensitivity == registry.High && t.Redact == nil {
			guarded = appendRoot(guarded, t.Src)
		}
		if t.LocalOnly {
			localOnly = appendRoot(localOnly, t.Src)
		}
	}
	// Roots already reported as left out: a wider entry or an include that
	// reaches the same files must not list them a second time.
	var reported []string
	for _, t := range targets {
		if !registry.Selected(t.Category, opts.Only, opts.Skip) {
			continue
		}
		// A plaintext backup must never hold an unredactable secret. A
		// high-sensitivity entry with no guaranteed redactor (e.g. ~/.gnupg,
		// cloud credentials) is excluded from a redacting backup, because
		// content scanning is best-effort and misses opaque tokens. An encrypted
		// backup (Redact off) carries them.
		if opts.Redact && t.Sensitivity == registry.High && t.Redact == nil {
			if _, err := os.Stat(t.Src); err == nil {
				res.SkippedSensitive = append(res.SkippedSensitive, t.Dest)
			}
			reported = appendRoot(reported, t.Src)
			continue
		}
		if opts.Remote && t.LocalOnly {
			if _, err := os.Stat(t.Src); err == nil {
				res.KeptLocal = append(res.KeptLocal, t.Dest)
			}
			reported = appendRoot(reported, t.Src)
			continue
		}
		if opts.Reading != nil {
			opts.Reading(t.Dest, false)
		}
		files, skipped := Walk(t, WalkOptions{MaxSize: opts.MaxFileSize, SkipVCS: opts.SkipVCS})
		for _, s := range skipped {
			switch {
			case s.Reason == "too large":
				res.TooLarge = append(res.TooLarge, s)
			case IsRebuildable(s):
				res.Rebuildable = append(res.Rebuildable, s)
			default:
				res.ReadErrors = append(res.ReadErrors, s.Dest)
			}
		}
		for _, f := range files {
			if opts.Context != nil && opts.Context.Err() != nil {
				return res, opts.Context.Err()
			}
			// Two entries can name the same file (~/.ssh/config is tracked on
			// its own and as part of ~/.ssh, or a user include overlaps the
			// registry); it is carried once, under the first entry's name.
			if written[f.Dest] || writtenSrc[f.Path] || reachesGuarded(t, f.Path, reported) {
				continue
			}
			if opts.Redact && reachesGuarded(t, f.Path, guarded) {
				res.Withheld = append(res.Withheld, f.Dest)
				continue
			}
			if opts.Remote && reachesGuarded(t, f.Path, localOnly) {
				res.KeptLocal = append(res.KeptLocal, f.Dest)
				continue
			}
			if opts.Reading != nil {
				opts.Reading(f.Dest, true)
			}
			raw, err := readRegular(f.Path, f.Size)
			if err != nil {
				res.ReadErrors = append(res.ReadErrors, f.Dest)
				continue
			}
			// An age identity under any name or path: cheap to look for, and
			// the one file a push must never carry.
			if opts.Remote && scan.ContainsAgeIdentity(raw) {
				res.KeptLocal = append(res.KeptLocal, f.Dest)
				continue
			}
			data := raw
			secret := false
			if !opts.Encrypted {
				if opts.Redact && len(raw) > maxGateText && !scan.LooksBinary(raw) {
					res.TooLarge = append(res.TooLarge, Skipped{Dest: f.Dest, Reason: "too large to check for secrets", Size: int64(len(raw))})
					continue
				}
				body, keep, sr := gate(f.Dest, string(raw), opts.Redact, t.Redact, &res.ScanResults)
				if !keep {
					res.Withheld = append(res.Withheld, f.Dest)
					continue
				}
				data = []byte(body)
				secret = sr.Action != scan.Include
			}
			var err2 error
			if cs, ok := sink.(ClassifyingSink); ok {
				sensitive := secret || t.Sensitivity != registry.Low || t.Redact != nil || reachesGuarded(t, f.Path, guarded)
				err2 = cs.AddClassified(f.Dest, data, f.Exec, sensitive)
			} else {
				err2 = sink.Add(f.Dest, data, f.Exec)
			}
			if err2 != nil {
				return res, err2 // a failed write to the destination is a real failure
			}
			written[f.Dest] = true
			writtenSrc[f.Path] = true
			res.PerCategory[t.Category]++
			res.TotalFiles++
			res.TotalBytes += int64(len(data))
			if res.SourceBytes == nil {
				res.SourceBytes = map[string]int64{}
			}
			res.SourceBytes[t.Src] += int64(len(data))
		}
	}
	if !opts.Redact {
		for _, sr := range res.ScanResults {
			if sr.Action == scan.Skip {
				res.RawSecrets = append(res.RawSecrets, sr.Path)
			}
		}
	}
	return res, nil
}

func under(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// appendRoot adds a guarded root, and also where it really is when it is a
// symlink: a stow-managed ~/.kube/config pointing into ~/.dotfiles is still a
// credential when ~/.dotfiles is what gets included.
func appendRoot(roots []string, src string) []string {
	c := filepath.Clean(src)
	roots = append(roots, c)
	if r, err := filepath.EvalSymlinks(c); err == nil && r != c {
		roots = append(roots, r)
	}
	return roots
}

// reachesGuarded reports whether a file inside a credential root was reached
// from outside it (through a user include, or an entry wrapping the root)
// rather than through a more specific registry entry that knows how to redact
// it (~/.ssh/config is its own entry inside the guarded ~/.ssh). Paths are
// compared as written and as resolved, so a symlink in either direction does
// not hide a credential.
func reachesGuarded(t registry.BackupTarget, file string, roots []string) bool {
	if len(roots) == 0 {
		return false
	}
	src := filepath.Clean(t.Src)
	check := func(file, src string) bool {
		for _, r := range roots {
			if !under(file, r) {
				continue
			}
			// Reached from outside the root: an include, an entry wrapping
			// it, or (resolved) any entry whose files are links into it. Only
			// an entry that is itself inside the root knows how to treat it.
			if t.Category == registry.ExtraCategory || !under(src, r) {
				return true
			}
		}
		return false
	}
	if check(file, src) {
		return true
	}
	rf, err1 := filepath.EvalSymlinks(file)
	rs, err2 := filepath.EvalSymlinks(src)
	if err1 != nil || err2 != nil || (rf == file && rs == src) {
		return false
	}
	return check(rf, rs)
}

// readRegular reads a file the walk vetted, refusing anything that is no
// longer a regular file (swapped for a FIFO since the walk, which would block
// the read forever) and never reading more than the walk measured plus slack,
// so a file growing under us cannot blow past the size cap.
func readRegular(p string, size int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", p)
	}
	limit := max(size, fi.Size()) + 1<<20
	return io.ReadAll(io.LimitReader(f, limit))
}

// gate applies the redaction/skip decision to one file's content. It returns the
// (possibly scrubbed) content and whether the file should be written at all. A
// skip-action finding (e.g. a private key) is never copied to a plaintext backup.
func gate(scanPath, body string, redact bool, entryRedact func(string) string, results *[]scan.Result) (string, bool, scan.Result) {
	sr := scan.ScanContentFull(scanPath, body)
	if redact && sr.Action == scan.Skip {
		*results = append(*results, sr)
		return "", false, sr
	}
	if redact && entryRedact != nil {
		body = entryRedact(body)
	}
	if redact {
		body = scan.ApplyRedactions(body, sr)
	}
	*results = append(*results, sr)
	return body, true, sr
}

// ManifestMeta is the run context recorded in a backup's MANIFEST.
type ManifestMeta struct {
	Host      string
	Home      string // the home folder files came from; restore rewrites it to the new one
	OS        string
	Version   string
	Created   string // pre-formatted timestamp
	Redacted  bool
	Encrypted bool
	Split     bool
}

// Manifest renders a self-describing MANIFEST for a backup: what was captured,
// what was deliberately left out, and how to restore it. A backup you can't
// audit for completeness is dangerous. The exclusion list is the
// safety-critical part, so it travels inside the backup rather than scrolling
// past once in the console.
func Manifest(meta ManifestMeta, res Result) string {
	var b strings.Builder
	b.WriteString("# dothaven backup\n#\n")
	fmt.Fprintf(&b, "# host:      %s\n", meta.Host)
	if meta.Home != "" {
		fmt.Fprintf(&b, "# home:      %s\n", meta.Home)
	}
	fmt.Fprintf(&b, "# os:        %s\n", meta.OS)
	fmt.Fprintf(&b, "# created:   %s\n", meta.Created)
	fmt.Fprintf(&b, "# dothaven:  %s\n", meta.Version)
	switch {
	case meta.Split:
		b.WriteString("# contents:  readable config as plain files; credentials and anything holding\n#            a secret are in secrets.tar.gz.age (age-encrypted)\n#\n")
	case meta.Encrypted:
		b.WriteString("# contents:  complete (secrets and keys kept), whole archive age-encrypted\n#\n")
	case meta.Redacted:
		b.WriteString("# contents:  secrets redacted, keys and credential files left out\n#\n")
	default:
		b.WriteString("# contents:  raw values kept, NOT encrypted; treat this backup as secret\n#\n")
	}
	b.WriteString("# Restore on a new machine with:\n#   dothaven restore <this backup>\n#\n")

	fmt.Fprintf(&b, "# Captured %d file(s):\n", res.TotalFiles)
	cats := make([]string, 0, len(res.PerCategory))
	for c := range res.PerCategory {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	for _, c := range cats {
		fmt.Fprintf(&b, "#   %s (%d)\n", c, res.PerCategory[c])
	}
	b.WriteString("#\n")

	left := false
	list := func(title string, dests []string) {
		if len(dests) == 0 {
			return
		}
		left = true
		b.WriteString(title)
		sorted := append([]string(nil), dests...)
		sort.Strings(sorted)
		for _, d := range sorted {
			fmt.Fprintf(&b, "#   %s\n", d)
		}
	}
	list("# Left out of this plaintext backup (credentials, high-sensitivity).\n"+
		"# Carry them encrypted: dothaven backup --encrypt  (or chezmoi-export --apply)\n",
		res.SkippedSensitive)
	list("# Left out: private keys found by the scan (same remedy as above).\n", res.Withheld)
	list("# Kept off GitHub, even encrypted: age keys, which open the encrypted files in\n"+
		"# a dotfiles repository. Carry them with dothaven backup --encrypt.\n", res.KeptLocal)
	var big []string
	for _, t := range res.TooLarge {
		big = append(big, fmt.Sprintf("%s (%d MiB)", t.Dest, t.Size>>20))
	}
	list("# Left out: over the per-file size cap.\n", big)
	list("# Left out: exist but could not be read.\n", res.ReadErrors)
	var rebuilt []string
	for _, r := range res.Rebuildable {
		rebuilt = append(rebuilt, fmt.Sprintf("%s (%s)", r.Dest, RebuildKind(r)))
	}
	list("# Left out: rebuilt on the new machine by the tool that made them\n"+
		"# (npm install, pip, your build).\n", rebuilt)
	if !left {
		b.WriteString("# Left out: nothing.\n")
	}
	return b.String()
}
