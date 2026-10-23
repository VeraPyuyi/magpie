package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

func TestDotGUIConnectionOwnsJournal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	dotbridge.SetCredentialStore(dotGUIVault{})
	dotbridge.SetJournalPath(filepath.Join(t.TempDir(), "journal.json"))
	t.Cleanup(func() { dotbridge.SetCredentialStore(nil); dotbridge.SetJournalPath("") })
	sub := dotbridge.Submission{RequestID: "same-request", Mode: "task", Task: "explicit task", SessionID: "session"}
	digest, _ := dotbridge.Digest(sub)
	var aCalls, bPosts, bGets, bCancels atomic.Int32
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { aCalls.Add(1); w.WriteHeader(500) }))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j := dotbridge.Job{Version: 1, JobID: "job-b", RequestID: sub.RequestID, RequestDigest: digest, Mode: sub.Mode, SessionID: sub.SessionID, State: "running"}
		switch {
		case r.URL.Path == "/dot-bridge/v1/status":
			now := time.Now()
			json.NewEncoder(w).Encode(dotbridge.Status{Version: 1, Enabled: true, Paired: true, Ready: true, VerifiedAt: now.Add(-time.Minute), ExpiresAt: new(now.Add(time.Hour)), Subscription: dotbridge.SubscriptionStatus{State: "active", ExpiresAt: new(now.Add(time.Hour))}, Capabilities: dotbridge.Capabilities{Tasks: true}})
		case r.Method == "POST" && r.URL.Path == "/dot-bridge/v1/jobs":
			bPosts.Add(1)
			j.State = "queued"
			w.WriteHeader(202)
			json.NewEncoder(w).Encode(struct {
				dotbridge.Job
				Accepted bool `json:"accepted"`
			}{j, true})
		case r.Method == "GET" && r.URL.Path == "/dot-bridge/v1/requests/same-request":
			bGets.Add(1)
			json.NewEncoder(w).Encode(j)
		case r.Method == "POST" && r.URL.Path == "/dot-bridge/v1/jobs/job-b/cancel":
			bCancels.Add(1)
			j.CancelRequested = true
			j.State = "cancel_requested"
			json.NewEncoder(w).Encode(j)
		default:
			w.WriteHeader(404)
		}
	}))
	defer b.Close()
	cfgA := dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(a.URL, "http://"), Credential: "pair-a", AnswerWaitSeconds: 120}
	cfgB := cfgA
	cfgB.Address = strings.TrimPrefix(b.URL, "http://")
	cfgB.Credential = "pair-b"
	if err := dotbridge.SaveConfig(cfgA); err != nil {
		t.Fatal(err)
	}
	cfgA, err := dotbridge.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	// An existing A record would make the buggy route query B instead of creating
	// B's new request. Its immutable A record must remain untouched.
	if _, _, err := dotbridge.Reserve(sub, dotGUIScope(cfgA)); err != nil {
		t.Fatal(err)
	}
	original, _ := dotbridge.Requests(dotGUIScope(cfgA))
	mux := http.NewServeMux()
	dotRoutesWithConnection(mux, func() (*dotbridge.Connection, error) {
		if err := dotbridge.SaveConfig(cfgB); err != nil {
			return nil, err
		}
		return dotbridge.PairedConnection()
	})
	body, _ := json.Marshal(sub)
	ask := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:3999/api/dot/"+path, strings.NewReader(string(body)))
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	// B must not query/cancel an A-only identity even when that exact request
	// exists in A's journal. Listing must also select B's namespace.
	w := ask("GET", "requests")
	if w.Code != 200 || strings.Contains(w.Body.String(), sub.RequestID) {
		t.Fatalf("A record exposed by B listing: %d %s", w.Code, w.Body)
	}
	w = ask("GET", "requests/same-request")
	if w.Code != 409 {
		t.Fatalf("A-only lookup under B: %d %s", w.Code, w.Body)
	}
	w = ask("POST", "requests/same-request/cancel")
	if w.Code != 409 || bGets.Load() != 0 || bCancels.Load() != 0 {
		t.Fatalf("A-only request reached B: gets=%d cancels=%d", bGets.Load(), bCancels.Load())
	}
	if err := dotbridge.SaveConfig(cfgA); err != nil {
		t.Fatal(err)
	}
	w = ask("POST", "jobs")
	if w.Code != 202 {
		t.Fatalf("submit: %d %s", w.Code, w.Body)
	}
	actualB, err := dotbridge.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	recordsB, err := dotbridge.Requests(dotGUIScope(actualB))
	if err != nil || len(recordsB) != 1 || recordsB[0].JobID != "job-b" {
		t.Fatalf("B submission missing from bound namespace: %+v %v", recordsB, err)
	}
	afterA, _ := dotbridge.Requests(dotGUIScope(cfgA))
	if len(afterA) != 1 || afterA[0] != original[0] {
		t.Fatalf("A journal changed: %+v", afterA)
	}
	if bPosts.Load() != 1 || bGets.Load() != 0 || aCalls.Load() != 0 {
		t.Fatalf("wrong pairing I/O: A=%d B posts=%d gets=%d", aCalls.Load(), bPosts.Load(), bGets.Load())
	}
	w = ask("GET", "requests")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "job-b") {
		t.Fatalf("B listing: %d %s", w.Code, w.Body)
	}
	w = ask("GET", "requests/same-request")
	if w.Code != 200 {
		t.Fatalf("B resume: %d %s", w.Code, w.Body)
	}
	w = ask("POST", "requests/same-request/cancel")
	if w.Code != 200 {
		t.Fatalf("B cancel: %d %s", w.Code, w.Body)
	}
	if bPosts.Load() != 1 || bGets.Load() != 2 || bCancels.Load() != 1 || aCalls.Load() != 0 {
		t.Fatal("recovery/cancel did not use bound B connection")
	}
	// A replacement after connection construction must fail before listing or
	// I/O too. The production client additionally checks again before Reserve.
	stale := http.NewServeMux()
	dotRoutesWithConnection(stale, func() (*dotbridge.Connection, error) {
		c, err := dotbridge.PairedConnection()
		if err != nil {
			return nil, err
		}
		if err := dotbridge.SaveConfig(cfgA); err != nil {
			return nil, err
		}
		return c, nil
	})
	for _, path := range []string{"status", "requests", "requests/same-request", "jobs", "requests/same-request/cancel"} {
		if err := dotbridge.SaveConfig(cfgB); err != nil {
			t.Fatal(err)
		}
		method := "GET"
		if path == "jobs" || strings.HasSuffix(path, "/cancel") {
			method = "POST"
		}
		r := httptest.NewRequest(method, "http://127.0.0.1:3999/api/dot/"+path, strings.NewReader(string(body)))
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		stale.ServeHTTP(w, r)
		if w.Code != 503 {
			t.Fatalf("stale connection %s: %d %s", path, w.Code, w.Body)
		}
	}
	if bPosts.Load() != 1 || bGets.Load() != 2 || bCancels.Load() != 1 || aCalls.Load() != 0 {
		t.Fatal("stale connection performed I/O")
	}
}
