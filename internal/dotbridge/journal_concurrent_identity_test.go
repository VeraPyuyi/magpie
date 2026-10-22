package dotbridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDotConcurrentResumeCannotReplaceDurableJob(t *testing.T) {
	SetJournalPath(filepath.Join(t.TempDir(), "journal.json"))
	t.Cleanup(func() { SetJournalPath("") })
	sub := Submission{RequestID: "req-concurrent", SessionID: "session", Mode: ModeAnswer, Messages: []Message{{Role: "user", Content: "hello"}}}
	snap, _, err := Reserve(sub, "scope")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var gets, posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			posts.Add(1)
			w.WriteHeader(500)
			return
		}
		n := gets.Add(1)
		id := "job-first"
		if n == 1 {
			close(entered)
			<-release
			id = "job-second"
		}
		_ = json.NewEncoder(w).Encode(Job{Version: 1, JobID: id, RequestID: sub.RequestID, SessionID: sub.SessionID, Mode: sub.Mode, RequestDigest: snap.Digest, State: StateCompleted, Result: &Result{Text: id}})
	}))
	defer srv.Close()
	c, err := NewConnection(strings.TrimPrefix(srv.URL, "http://"), func() (string, error) { return "fixture", nil })
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, _, err := c.Resume(t.Context(), "scope", sub.RequestID); done <- err }()
	<-entered
	job, _, err := c.Resume(t.Context(), "scope", sub.RequestID)
	if err != nil || job.JobID != "job-first" {
		t.Fatalf("first durable receipt %+v %v", job, err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Error("late conflicting receipt returned result instead of journal conflict")
	}
	items, err := Requests("scope")
	if err != nil || len(items) != 1 || items[0].JobID != "job-first" {
		t.Fatalf("durable original replaced %+v %v", items, err)
	}
	if posts.Load() != 0 {
		t.Fatal("Resume posted")
	}
	if err := SaveAttempt("scope", snap); err == nil {
		t.Fatal("stale empty reservation cleared durable job")
	}
}
