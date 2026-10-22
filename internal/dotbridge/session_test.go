package dotbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestDotEmptySessionReceiptAndLookup(t *testing.T) {
	sub := testSubmission()
	sub.SessionID = ""
	digest, _ := Digest(sub)
	job := testJob(sub, digest)
	var posts, lookups atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer paired" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/dot-bridge/v1/jobs":
			posts.Add(1)
			var received Submission
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if received.SessionID != "" {
				t.Error("submitted session changed")
			}
			w.WriteHeader(202)
			_ = json.NewEncoder(w).Encode(submitEnvelope{Job: job, Accepted: true})
		case r.Method == http.MethodGet && r.URL.Path == "/dot-bridge/v1/requests/"+sub.RequestID:
			lookups.Add(1)
			_ = json.NewEncoder(w).Encode(job)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	SetJournalPath(filepath.Join(t.TempDir(), "journal.json"))
	c := newTestConnection(t, srv, "paired")
	a, err := NewAttempt(sub)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Enqueue(context.Background(), sub, "empty-session", a)
	if err != nil {
		t.Fatalf("202 receipt: %v", err)
	}
	if got.SessionID != "" || !a.Snapshot().BlockReplay || a.Snapshot().State != attemptAccepted {
		t.Fatal("empty-session acceptance lost")
	}
	got, err = c.ByRequest(context.Background(), a)
	if err != nil || got.JobID != job.JobID {
		t.Fatalf("lookup: job=%+v err=%v", got, err)
	}
	got, resumed, err := c.Resume(context.Background(), "empty-session", sub.RequestID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got.JobID != job.JobID || resumed.Snapshot().SessionID != "" || !resumed.Snapshot().BlockReplay {
		t.Fatal("resume changed identity/barrier")
	}
	if posts.Load() != 1 || lookups.Load() != 2 {
		t.Fatalf("posts=%d lookups=%d", posts.Load(), lookups.Load())
	}
}

func TestDotSessionMismatchReceipt(t *testing.T) {
	for _, submitted := range []string{"", "sess-original"} {
		for _, receipt := range []string{"", "sess-other"} {
			if submitted == receipt {
				continue
			}
			t.Run(submitted+"/"+receipt, func(t *testing.T) {
				sub := testSubmission()
				sub.SessionID = submitted
				digest, _ := Digest(sub)
				job := testJob(sub, digest)
				job.SessionID = receipt
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost {
						w.WriteHeader(202)
						_ = json.NewEncoder(w).Encode(submitEnvelope{Job: job, Accepted: true})
						return
					}
					_ = json.NewEncoder(w).Encode(job)
				}))
				defer srv.Close()
				c := newTestConnection(t, srv, "paired")
				a, err := NewAttempt(sub)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = c.Submit(context.Background(), sub, a); err == nil {
					t.Fatal("mismatched submit receipt accepted")
				}
				if _, err = c.ByRequest(context.Background(), a); err == nil {
					t.Fatal("mismatched lookup receipt accepted")
				}
				snap := a.Snapshot()
				if !snap.BlockReplay || snap.State != attemptAcceptanceUnknown || snap.JobID != "" {
					t.Fatalf("mismatch changed barrier: %+v", snap)
				}
			})
		}
	}
}
