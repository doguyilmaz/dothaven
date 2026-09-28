package registry

// BackupTarget is a registry entry projected onto a backup source→destination
// for the current platform. Redact is the entry's optional content scrubber.
type BackupTarget struct {
	Src         string
	Dest        string
	Category    string
	IsDir       bool
	Redact      func(string) string
	Sensitivity Sensitivity
	Exclude     []string // Dir only: patterns dropped from the walk (see backup.Excluded)
	LocalOnly   bool     // never pushed off this machine (see Entry.LocalOnly)
}

// BackupTargets resolves every entry with a path on this platform into a backup
// target. It is the single projection of the registry consumed by both backup
// (to copy) and restore (to map a backed-up file back to its live path).
func BackupTargets(home string, entries []Entry) []BackupTarget {
	out := make([]BackupTarget, 0, len(entries))
	for _, e := range entries {
		src := ResolvePath(e, home)
		if src == "" {
			continue
		}
		out = append(out, BackupTarget{
			Src:         src,
			Dest:        e.BackupDest,
			Category:    e.Category,
			IsDir:       e.Kind == Dir,
			Redact:      e.Redact,
			Sensitivity: e.Sensitivity,
			Exclude:     e.Exclude,
			LocalOnly:   e.LocalOnly,
		})
	}
	return out
}
