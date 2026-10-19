package dotbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/edit"
)

func TestDotReviewOffPreservesPendingNamespace(t *testing.T) {
	c, cfg := identityFixture(t, identityReadyHandler())
	_ = c
	SetJournalPath(t.TempDir() + "/journal.json")
	t.Cleanup(func() { SetJournalPath("") })
	scope := "cli:" + PairingIdentity(cfg)
	sub := testSubmission()
	if _, _, err := Reserve(sub, scope); err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = false
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	off, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if PairingIdentity(off) != PairingIdentity(cfg) {
		t.Fatal("off stranded pending pairing namespace")
	}
	rows, err := Requests("cli:" + PairingIdentity(off))
	if err != nil || len(rows) != 1 {
		t.Fatalf("off pending list=%v %v", rows, err)
	}
	off.Enabled = true
	if err := SaveConfig(off); err != nil {
		t.Fatal(err)
	}
	on, _ := LoadConfig()
	if PairingIdentity(on) != PairingIdentity(cfg) {
		t.Fatal("reenable changed pending wire namespace")
	}
}

func TestDotReviewSwitchBeforeReserveLeavesNoRecord(t *testing.T) {
	var posts atomic.Int32
	c, cfg := identityFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts.Add(1); w.WriteHeader(500) }))
	SetJournalPath(t.TempDir() + "/journal.json")
	t.Cleanup(func() { SetJournalPath("") })
	scope := "cli:" + PairingIdentity(cfg)
	if err := SavePairing(cfg, "new-secret"); err != nil {
		t.Fatal(err)
	}
	a, _ := NewAttempt(testSubmission())
	_, err := c.Enqueue(context.Background(), testSubmission(), scope, a)
	if err == nil {
		t.Fatal("obsolete pairing accepted")
	}
	rows, e := Requests(scope)
	if e != nil || len(rows) != 0 || posts.Load() != 0 || a.Snapshot().BlockReplay {
		t.Fatalf("obsolete pairing reserved/sent: rows=%v posts=%d snapshot=%+v err=%v", rows, posts.Load(), a.Snapshot(), e)
	}
}

func TestDotReviewConnectionRejectsForeignScope(t *testing.T) {
	c, cfg := identityFixture(t, identityReadyHandler())
	SetJournalPath(t.TempDir() + "/journal.json")
	t.Cleanup(func() { SetJournalPath("") })
	foreign := cfg
	foreign.PairingGeneration = "old-generation"
	a, _ := NewAttempt(testSubmission())
	_, err := c.Enqueue(context.Background(), testSubmission(), "cli:"+PairingIdentity(foreign), a)
	if err == nil || a.Snapshot().BlockReplay {
		t.Fatal("current connection reserved foreign pairing scope")
	}
	rows, _ := Requests("cli:" + PairingIdentity(foreign))
	if len(rows) != 0 {
		t.Fatal("foreign journal mutated")
	}
	if !strings.HasPrefix(c.PairingIdentity(), "pair-") {
		t.Fatal("missing pairing identity")
	}
}

func TestDotReviewOffResumeUsesOriginalWireID(t *testing.T) {
	sub := testSubmission()
	digest, _ := Digest(sub)
	var gets atomic.Int32
	c, cfg := identityFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/dot-bridge/v1/requests/"+sub.RequestID {
			t.Errorf("unexpected wire request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		gets.Add(1)
		writeIdentityJob(w, testJob(sub, digest))
	}))
	_ = c
	SetJournalPath(t.TempDir() + "/journal.json")
	t.Cleanup(func() { SetJournalPath("") })
	scope := "cli:" + PairingIdentity(cfg)
	if _, _, err := Reserve(sub, scope); err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = false
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	conn, err := PairedConnection()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = conn.Resume(context.Background(), "cli:"+conn.PairingIdentity(), sub.RequestID)
	if err != nil || gets.Load() != 1 {
		t.Fatalf("resume original job: gets=%d err=%v", gets.Load(), err)
	}
}

func writeIdentityJob(w http.ResponseWriter, j Job) { _ = json.NewEncoder(w).Encode(j) }

func TestDotReviewPostSwitchKeepsBarrier(t *testing.T) {
	var cfg Config
	var posts atomic.Int32
	c, loaded := identityFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		replacement := cfg
		replacement.PairingGeneration = "external-replacement"
		raw, _ := json.Marshal(replacement)
		if err := edit.WriteAtomic(configPath(), raw); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{}`))
	}))
	cfg = loaded
	SetJournalPath(t.TempDir() + "/journal.json")
	t.Cleanup(func() { SetJournalPath("") })
	a, _ := NewAttempt(testSubmission())
	_, err := c.Enqueue(t.Context(), testSubmission(), "cli:"+PairingIdentity(cfg), a)
	if err == nil || posts.Load() != 1 || !a.Snapshot().BlockReplay {
		t.Fatalf("post-switch barrier lost: err=%v posts=%d snap=%+v", err, posts.Load(), a.Snapshot())
	}
}

func TestDotReviewExternalOffCycleInvalidatesProofOnly(t *testing.T) {
	c, cfg := identityFixture(t, identityReadyHandler())
	if _, err := c.Status(t.Context()); err != nil {
		t.Fatal(err)
	}
	// An independent writer retains durable identity but records an off cycle.
	cfg.ReadinessRevision = "external-off-cycle"
	raw, _ := json.Marshal(cfg)
	if err := edit.WriteAtomic(configPath(), raw); err != nil {
		t.Fatal(err)
	}
	if CachedReady(time.Now()) {
		t.Fatal("external off cycle restored stale proof")
	}
	if PairingIdentity(cfg) != c.PairingIdentity() {
		t.Fatal("off cycle changed durable identity")
	}
}

// Fixtures use only loopback servers and isolated credential stores.
