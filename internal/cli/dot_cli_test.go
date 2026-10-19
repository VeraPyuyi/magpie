package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

type dotFakeStore struct {
	token  string
	writes int
}

func (s *dotFakeStore) Get(_, _ string) (string, error) { return s.token, nil }
func (s *dotFakeStore) Set(_, _, v string) error        { s.token = v; s.writes++; return nil }
func (s *dotFakeStore) Delete(_, _ string) error        { s.token = ""; return nil }
func dotSandbox(t *testing.T) *dotFakeStore {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	s := &dotFakeStore{token: "fixture-secret"}
	dotbridge.SetCredentialStore(s)
	t.Cleanup(func() { dotbridge.SetCredentialStore(nil) })
	dotbridge.SetJournalPath(filepath.Join(home, "requests.json"))
	t.Cleanup(func() { dotbridge.SetJournalPath("") })
	return s
}
func TestDotCLIHelpDispatch(t *testing.T) {
	dotSandbox(t)
	if err := run([]string{"dot", "--help"}); err != nil {
		t.Fatal(err)
	}
	if got := helpFor([]string{"dot"}); !strings.Contains(got, "pair <pairing-file>") || !strings.Contains(got, "request <requestID>") {
		t.Fatalf("missing dot operator help: %s", got)
	}
}
func TestDotCLITaskGateAndRecovery(t *testing.T) {
	dotSandbox(t)
	var enabled atomic.Bool
	var posts atomic.Int32
	sub := dotbridge.Submission{RequestID: "cli-request-1", Mode: dotbridge.ModeTask, Task: "Summarize the supplied context", Messages: []dotbridge.Message{}}
	digest, _ := dotbridge.Digest(sub)
	job := dotbridge.Job{Version: 1, JobID: "job-1", RequestID: sub.RequestID, RequestDigest: digest, Mode: sub.Mode, State: "queued"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Error("missing authentication")
		}
		switch {
		case r.URL.Path == "/dot-bridge/v1/status":
			json.NewEncoder(w).Encode(dotbridge.Status{Version: 1, Enabled: true, Paired: true, Ready: true, VerifiedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Subscription: dotbridge.SubscriptionStatus{State: "active", ExpiresAt: time.Now().Add(time.Hour)}, Capabilities: dotbridge.Capabilities{Tasks: enabled.Load()}})
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			posts.Add(1)
			j := job
			j.State = "running"
			j.CancelRequested = true
			json.NewEncoder(w).Encode(j)
		case r.Method == http.MethodPost:
			posts.Add(1)
			w.WriteHeader(202)
			json.NewEncoder(w).Encode(struct {
				dotbridge.Job
				Accepted bool `json:"accepted"`
			}{job, true})
		default:
			json.NewEncoder(w).Encode(job)
		}
	}))
	defer srv.Close()
	if err := dotbridge.SaveConfig(dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(srv.URL, "http://"), Credential: "cli"}); err != nil {
		t.Fatal(err)
	}
	path := dotTempFile(t, "task.json")
	dotWriteFile(t, path, []byte(`{"request_id":"cli-request-1","mode":"task","task":"Summarize the supplied context","session_id":"","messages":[]}`))
	dotTestPrivate(t, path)
	var out bytes.Buffer
	err := dotTo(context.Background(), &out, []string{"submit", "--file", path})
	if err == nil || !strings.Contains(err.Error(), "probe_required") || posts.Load() != 0 {
		t.Fatalf("task gate err=%v posts=%d", err, posts.Load())
	}
	enabled.Store(true)
	out.Reset()
	if err := dotTo(context.Background(), &out, []string{"submit", "--file", path}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "job-1") || !strings.Contains(out.String(), "queued") || posts.Load() != 1 {
		t.Fatal(out.String(), posts.Load())
	}
	if err := dotTo(context.Background(), &out, []string{"request", sub.RequestID}); err != nil {
		t.Fatal(err)
	}
	if err := dotTo(context.Background(), &out, []string{"submit", "--file", path}); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 {
		t.Fatal("recovery posted again")
	}
	out.Reset()
	if err := dotTo(context.Background(), &out, []string{"cancel", "job-1"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "intent") || strings.Contains(out.String(), "confirmed") {
		t.Fatal(out.String())
	}
}
func TestDotCLIPairRejectsBeforeVault(t *testing.T) {
	store := dotSandbox(t)
	for i, body := range []string{`{"version":2}`, `{"version":1,"token":"do-not-print","listen":"https://127.0.0.1:1234"}`, strings.Repeat("x", 16385)} {
		p := dotTempFile(t, fmt.Sprintf("pair-%d.json", i))
		dotWriteFile(t, p, []byte(body))
		var out bytes.Buffer
		err := dotTo(context.Background(), &out, []string{"pair", p})
		if err == nil || store.writes != 0 || strings.Contains(out.String(), "do-not-print") || strings.Contains(err.Error(), "do-not-print") {
			t.Fatal(err, out.String(), store.writes)
		}
	}
}
