package dotbridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/filelock"
	"github.com/yetone/magpie/internal/settings"
)

type journalRecord struct {
	Scope     string `json:"scope"`
	RequestID string `json:"request_id"`
	Digest    string `json:"digest"`
	Mode      string `json:"mode"`
	SessionID string `json:"session_id"`
	State     string `json:"state"`
	JobID     string `json:"job_id,omitempty"`
	BlockReplay bool `json:"block_replay"`
}

type journalFile struct {
	Version int             `json:"version"`
	Records []journalRecord `json:"records"`
}

var (
	journalPathOverride string
	journalMu           sync.Mutex
)

func journalPath() string {
	if journalPathOverride != "" {
		return journalPathOverride
	}
	return filepath.Join(settings.Dir(), "dot-bridge-requests.json")
}

func journalLockPath() string {
	return journalPath() + ".lock"
}

// SetJournalPath overrides the journal location (tests only).
func SetJournalPath(path string) {
	journalMu.Lock()
	journalPathOverride = path
	journalMu.Unlock()
}

func readJournal() (journalFile, error) {
	path := journalPath()
	b, err := edit.Read(path)
	if err != nil {
		return journalFile{}, errJournal
	}
	if b == nil {
		return journalFile{Version: 1}, nil
	}
	var file journalFile
	if err := json.Unmarshal(b, &file); err != nil {
		return journalFile{}, errJournal
	}
	if file.Version == 0 {
		file.Version = 1
	}
	return file, nil
}

func writeJournal(file journalFile) error {
	if err := os.MkdirAll(filepath.Dir(journalPath()), 0o700); err != nil {
		return err
	}
	file.Version = 1
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return edit.WriteAtomic(journalPath(), append(b, '\n'))
}

func recordToSnapshot(rec journalRecord) Snapshot {
	return Snapshot{
		RequestID:   rec.RequestID,
		Digest:      rec.Digest,
		JobID:       rec.JobID,
		State:       rec.State,
		Mode:        rec.Mode,
		SessionID:   rec.SessionID,
		BlockReplay: rec.BlockReplay,
	}
}

func withJournal(fn func(*journalFile) error) error {
	unlock, held := filelock.Lock(journalLockPath(), filelock.Wait)
	if !held {
		return errJournal
	}
	defer unlock()
	file, err := readJournal()
	if err != nil {
		return err
	}
	if err := fn(&file); err != nil {
		return err
	}
	return writeJournal(file)
}

// Reserve atomically records identity before I/O.
func Reserve(sub Submission, scope string) (Snapshot, bool, error) {
	if err := validateSubmission(sub); err != nil {
		return Snapshot{}, false, err
	}
	digest, err := Digest(sub)
	if err != nil {
		return Snapshot{}, false, err
	}
	var snap Snapshot
	var existing bool
	err = withJournal(func(file *journalFile) error {
		for _, rec := range file.Records {
			if rec.Scope != scope || rec.RequestID != sub.RequestID {
				continue
			}
			if rec.Digest != digest {
				return errDigestConflict
			}
			snap = recordToSnapshot(rec)
			existing = true
			return nil
		}
		if len(file.Records) >= maxJournalRecords {
			return errJournal
		}
		rec := journalRecord{
			Scope:       scope,
			RequestID:   sub.RequestID,
			Digest:      digest,
			Mode:        sub.Mode,
			SessionID:   sub.SessionID,
			State:       attemptAcceptanceUnknown,
			BlockReplay: true,
		}
		file.Records = append(file.Records, rec)
		snap = recordToSnapshot(rec)
		existing = false
		return nil
	})
	if err != nil {
		return Snapshot{}, false, err
	}
	return snap, existing, nil
}

// SaveAttempt persists the latest attempt snapshot for a scope.
func SaveAttempt(scope string, snapshot Snapshot) error {
	if scope == "" || snapshot.RequestID == "" {
		return errInvalidBody
	}
	return withJournal(func(file *journalFile) error {
		for i, rec := range file.Records {
			if rec.Scope != scope || rec.RequestID != snapshot.RequestID {
				continue
			}
			if rec.Digest != snapshot.Digest {
				return errDigestConflict
			}
			file.Records[i].State = snapshot.State
			file.Records[i].JobID = snapshot.JobID
			file.Records[i].BlockReplay = snapshot.BlockReplay
			return nil
		}
		return errJournal
	})
}

// Requests returns journal snapshots for one caller scope.
func Requests(scope string) ([]Snapshot, error) {
	unlock, held := filelock.Lock(journalLockPath(), filelock.Wait)
	if !held {
		return nil, errJournal
	}
	defer unlock()
	file, err := readJournal()
	if err != nil {
		return nil, err
	}
	var out []Snapshot
	for _, rec := range file.Records {
		if rec.Scope == scope {
			out = append(out, recordToSnapshot(rec))
		}
	}
	return out, nil
}

// lookupRequest finds one journal record.
func lookupRequest(scope, requestID string) (Snapshot, error) {
	unlock, held := filelock.Lock(journalLockPath(), filelock.Wait)
	if !held {
		return Snapshot{}, errJournal
	}
	defer unlock()
	file, err := readJournal()
	if err != nil {
		return Snapshot{}, err
	}
	for _, rec := range file.Records {
		if rec.Scope == scope && rec.RequestID == requestID {
			return recordToSnapshot(rec), nil
		}
	}
	return Snapshot{}, errors.New("dot_bridge_request_not_found")
}
