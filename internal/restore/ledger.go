package restore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"time"

	"github.com/doguyilmaz/dothaven/internal/sys"
)

// Ledger remembers what restore did on this machine, so running it again shows
// what is already applied, what you changed since, and what you chose to skip —
// instead of offering every file as new work each time.
//
// It holds hashes, never content: it lives beside backups in the data
// directory, and a hash of a file says nothing about what was in it.
type Ledger struct {
	Version int `json:"version"`
	// Applied is keyed by the live path written.
	Applied map[string]AppliedFile `json:"applied"`
	// Skipped is keyed by backup, then by the file's path inside it, and holds
	// the hash of the backup's copy that was declined. A later backup with a
	// different copy is a new question.
	Skipped map[string]map[string]string `json:"skipped"`
}

// AppliedFile is one write restore made.
type AppliedFile struct {
	SHA256 string    `json:"sha256"`
	Backup string    `json:"backup"`
	Dest   string    `json:"dest"`
	At     time.Time `json:"at"`
}

// Hash is the ledger's content hash.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// NewLedger is an empty ledger.
func NewLedger() *Ledger {
	return &Ledger{Version: 1, Applied: map[string]AppliedFile{}, Skipped: map[string]map[string]string{}}
}

// LoadLedger reads the ledger at path; a missing or unreadable one is empty
// (the only cost is that everything looks unapplied once).
func LoadLedger(path string) *Ledger {
	l := NewLedger()
	b, err := os.ReadFile(path)
	if err != nil {
		return l
	}
	if json.Unmarshal(b, l) != nil {
		return NewLedger()
	}
	if l.Applied == nil {
		l.Applied = map[string]AppliedFile{}
	}
	if l.Skipped == nil {
		l.Skipped = map[string]map[string]string{}
	}
	return l
}

// Save writes the ledger owner-only and atomically.
func (l *Ledger) Save(path string) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return sys.WriteFileSecure(path, string(b)+"\n")
}

// Record folds a restore's outcomes into the ledger: what was written, and
// what was looked at and declined.
func (l *Ledger) Record(backupID string, outcomes []Outcome, now time.Time) {
	for _, o := range outcomes {
		switch {
		case o.Written:
			l.Applied[o.Entry.TargetPath] = AppliedFile{SHA256: o.Entry.BackupSHA, Backup: backupID, Dest: o.Entry.BackupPath, At: now}
			if s := l.Skipped[backupID]; s != nil {
				delete(s, o.Entry.BackupPath)
			}
		case o.Declined:
			if l.Skipped[backupID] == nil {
				l.Skipped[backupID] = map[string]string{}
			}
			l.Skipped[backupID][o.Entry.BackupPath] = o.Entry.BackupSHA
		}
	}
}

// refine sharpens a content-only status with what the ledger knows.
func (l *Ledger) refine(e *Entry, backupID string) {
	if l == nil || e.Status == StatusRedacted {
		return
	}
	if a, ok := l.Applied[e.TargetPath]; ok {
		switch {
		case e.Status == StatusSame && a.SHA256 == e.BackupSHA:
			e.AppliedAt = a.At
			return
		case e.Status == StatusConflict && a.SHA256 == e.LiveSHA:
			// The file here is exactly what restore wrote before, untouched
			// since; the backup simply has a newer copy. Nothing of the
			// user's would be lost by updating it.
			e.Status = StatusUpdate
		case e.Status == StatusConflict:
			e.Status = StatusChanged
			e.AppliedAt = a.At
		}
	}
	if e.Status == StatusSame {
		return
	}
	if s := l.Skipped[backupID]; s != nil && s[e.BackupPath] == e.BackupSHA {
		e.Status = StatusSkipped
	}
}
