package gui

import (
	"encoding/json"
	"github.com/yetone/magpie/internal/dotbridge"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDotGatewayRouterLocalOffRecovery(t *testing.T) {
	dotbridge.SetJournalPath(filepath.Join(t.TempDir(), "journal.json"))
	dotbridge.SetCredentialStore(dotGUIVault{})
	t.Cleanup(func() { dotbridge.SetJournalPath(""); dotbridge.SetCredentialStore(nil) })
	sub := dotbridge.Submission{RequestID: "original", SessionID: "session", Mode: "task", Task: "private task"}
	digest, _ := dotbridge.Digest(sub)
	var gets, posts atomic.Int32
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			posts.Add(1)
			w.WriteHeader(405)
			return
		}
		gets.Add(1)
		json.NewEncoder(w).Encode(dotbridge.Job{Version: 1, RequestID: sub.RequestID, SessionID: sub.SessionID, RequestDigest: digest, JobID: "job", Mode: "task", State: "completed", Result: &dotbridge.Result{Text: "<script>literal</script>"}})
	}))
	defer bridge.Close()
	cfg := dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(bridge.URL, "http://"), Credential: "fixture"}
	if err := dotbridge.SavePairing(cfg, "gui-fixture-secret"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = dotbridge.LoadConfig()
	scope := dotbridge.PairingIdentity(cfg) + ":" + strings.Repeat("a", 64)
	if _, _, err := dotbridge.Reserve(sub, scope); err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = false
	if err := dotbridge.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	handler := ownWindow(Handler(nil, nil))
	for _, tc := range []struct {
		method, path string
		want         int
	}{{"GET", "gateway/requests/original", 200}, {"POST", "gateway/requests/original", 405}, {"POST", "gateway/requests/original/cancel", 405}} {
		r := httptest.NewRequest(tc.method, "http://localhost/api/dot/"+tc.path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s => %d %s", tc.method, tc.path, w.Code, w.Body)
		}
		if w.Code == 200 {
			for _, s := range []string{"request_digest", "original_fingerprint", "scope", "private task"} {
				if strings.Contains(w.Body.String(), s) {
					t.Fatalf("leaked %s", s)
				}
			}
		}
	}
	if gets.Load() != 1 || posts.Load() != 0 {
		t.Fatalf("gets=%d posts=%d", gets.Load(), posts.Load())
	}
	// Duplicate identity in another current gateway scope fails closed before RPC.
	if _, _, err := dotbridge.Reserve(sub, dotbridge.PairingIdentity(cfg)+":"+strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost/api/dot/gateway/requests/original", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 409 || gets.Load() != 1 {
		t.Fatalf("ambiguity %d gets=%d %s", w.Code, gets.Load(), w.Body)
	}
}
