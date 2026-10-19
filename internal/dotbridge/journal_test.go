package dotbridge

import (
	"os"
	"testing"
)

func TestDotJournalIdentity(t *testing.T) {
	path := t.TempDir() + "/journal.json"
	SetJournalPath(path)
	sub := testSubmission()
	snap, existing, err := Reserve(sub, "scope-j")
	if err != nil || existing {
		t.Fatalf("first reserve: existing=%v err=%v", existing, err)
	}
	if snap.State != attemptAcceptanceUnknown || !snap.BlockReplay {
		t.Fatalf("unexpected first snapshot %+v", snap)
	}
	sub2 := sub
	sub2.Messages = []Message{{Role: "user", Content: "changed"}}
	_, _, err = Reserve(sub2, "scope-j")
	if err != errDigestConflict {
		t.Fatalf("expected digest conflict, got %v", err)
	}

	SetJournalPath(t.TempDir() + "/broken.json")
	if err := os.WriteFile(journalPath(), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = Reserve(sub, "scope-x")
	if err != errJournal {
		t.Fatalf("corrupt journal must fail closed: %v", err)
	}
}

func TestDotJournalResumeReadsUnknown(t *testing.T) {
	SetJournalPath(t.TempDir() + "/journal.json")
	sub := testSubmission()
	_, _, err := Reserve(sub, "scope-r")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := Requests("scope-r")
	if err != nil || len(rows) != 1 || rows[0].State != attemptAcceptanceUnknown {
		t.Fatalf("unexpected rows %+v err=%v", rows, err)
	}
}
