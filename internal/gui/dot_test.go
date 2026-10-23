package gui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

type dotGUIVault struct{}

func (dotGUIVault) Get(string, string) (string, error) { return "gui-fixture-secret", nil }
func (dotGUIVault) Set(string, string, string) error   { return nil }
func (dotGUIVault) Delete(string, string) error        { return nil }

func TestDotGUIHandler(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	dotbridge.SetCredentialStore(dotGUIVault{})
	dotbridge.SetJournalPath(filepath.Join(t.TempDir(), "requests.json"))
	t.Cleanup(func() { dotbridge.SetCredentialStore(nil); dotbridge.SetJournalPath("") })
	var mu sync.Mutex
	tasks, unknown, wrongCancel := false, false, false
	posts, gets, cancels := 0, 0, 0
	var job dotbridge.Job
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer gui-fixture-secret" {
			t.Error("missing paired credential")
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/dot-bridge/v1/status":
			now := time.Now()
			json.NewEncoder(w).Encode(dotbridge.Status{Version: 1, Enabled: true, Paired: true, Ready: true, VerifiedAt: now.Add(-time.Minute), Subscription: dotbridge.SubscriptionStatus{State: "active"}, Capabilities: dotbridge.Capabilities{Text: true, Tasks: tasks}})
		case r.Method == "POST" && r.URL.Path == "/dot-bridge/v1/jobs":
			posts++
			var sub dotbridge.Submission
			json.NewDecoder(r.Body).Decode(&sub)
			digest, _ := dotbridge.Digest(sub)
			job = dotbridge.Job{Version: 1, JobID: "job-gui", RequestID: sub.RequestID, RequestDigest: digest, Mode: sub.Mode, SessionID: sub.SessionID, State: "queued"}
			w.WriteHeader(202)
			if unknown {
				fmt.Fprint(w, `{`)
				return
			}
			json.NewEncoder(w).Encode(struct {
				dotbridge.Job
				Accepted bool `json:"accepted"`
			}{job, true})
		case r.Method == "GET" && r.URL.Path == "/dot-bridge/v1/requests/req-gui":
			gets++
			json.NewEncoder(w).Encode(job)
		case r.Method == "POST" && r.URL.Path == "/dot-bridge/v1/jobs/job-gui/cancel":
			cancels++
			j := job
			j.CancelRequested = true
			j.State = "cancel_requested"
			if wrongCancel {
				j.RequestDigest = "wrong"
			}
			json.NewEncoder(w).Encode(j)
		default:
			w.WriteHeader(404)
		}
	}))
	defer bridge.Close()
	cfg := dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(bridge.URL, "http://"), Credential: "gui-fixture", AnswerWaitSeconds: 120}
	if err := dotbridge.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	handler := Handler(nil, nil)
	ask := func(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:3999/api/dot/"+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:1234"
		for k, v := range headers {
			if k == "Host" {
				r.Host = v
			} else if k == "RemoteAddr" {
				r.RemoteAddr = v
			} else {
				r.Header.Set(k, v)
			}
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"request_id":"req-gui","mode":"task","session_id":"session-gui","task":"Write a report","messages":[{"role":"user","content":"Only this explicit context"}]}`
	for _, headers := range []map[string]string{{"Origin": "null"}, {"Origin": "http://evil.test"}, {"Host": "evil.test"}, {"Host": "sub.localhost"}, {"RemoteAddr": "192.168.1.5:1234"}, {"Forwarded": "for=127.0.0.1"}, {"X-Forwarded-Host": "localhost"}, {"Authorization": "Bearer caller-key"}, {"Sec-Fetch-Site": "cross-site"}} {
		w := ask("POST", "jobs", body, headers)
		if w.Code != 403 {
			t.Fatalf("guard %v: %d %s", headers, w.Code, w.Body)
		}
	}
	status := ask("GET", "status", "", nil)
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"subscription_term":"permanent"`) || !strings.Contains(status.Body.String(), `"text_available":true`) || strings.Contains(status.Body.String(), "0001-01-01") {
		t.Fatalf("permanent status: %d %s", status.Code, status.Body)
	}
	w := ask("POST", "jobs", body, nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "probe_required") {
		t.Fatalf("task capability gate: %d %s", w.Code, w.Body)
	}
	mu.Lock()
	if posts != 0 {
		t.Fatal("unsupported task sent POST")
	}
	tasks = true
	unknown = true
	mu.Unlock()
	w = ask("POST", "jobs", body, nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "outcome_unknown") {
		t.Fatalf("uncertain receipt: %d %s", w.Code, w.Body)
	}
	// A second same-identity submission and explicit recovery only GET the
	// original request. They cannot enqueue a second background task.
	w = ask("POST", "jobs", body, nil)
	if w.Code != 202 {
		t.Fatalf("recover existing: %d %s", w.Code, w.Body)
	}
	w = ask("GET", "requests/req-gui", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "job-gui") {
		t.Fatalf("resume: %d %s", w.Code, w.Body)
	}
	w = ask("GET", "requests/foreign", "", nil)
	if w.Code != 409 {
		t.Fatalf("foreign request: %d", w.Code)
	}
	mu.Lock()
	if posts != 1 || gets != 2 {
		t.Fatalf("posts=%d gets=%d", posts, gets)
	}
	mu.Unlock()
	// CLI journal entries never appear in GUI history.
	dotbridge.Reserve(dotbridge.Submission{RequestID: "req-cli", Mode: "task", Task: "private"}, "cli:other")
	w = ask("GET", "requests", "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "req-cli") || strings.Contains(w.Body.String(), "Write a report") || !strings.Contains(w.Body.String(), "req-gui") {
		t.Fatalf("scoped identity history: %s", w.Body)
	}
	w = ask("POST", "requests/req-gui/cancel", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "cancel_intent_acknowledged") {
		t.Fatalf("cancel: %d %s", w.Code, w.Body)
	}
	mu.Lock()
	wrongCancel = true
	mu.Unlock()
	w = ask("POST", "requests/req-gui/cancel", "", nil)
	if w.Code != 409 || strings.Contains(w.Body.String(), "acknowledged") {
		t.Fatalf("mismatched cancel receipt: %d %s", w.Code, w.Body)
	}
	w = ask("GET", "status", "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "gui-fixture-secret") || strings.Contains(w.Body.String(), cfg.Credential) {
		t.Fatalf("status: %d %s", w.Code, w.Body)
	}
	w = ask("POST", "off", "", nil)
	if w.Code != 200 {
		t.Fatalf("off: %d", w.Code)
	}
	w = ask("GET", "status", "", nil)
	if !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("disabled: %s", w.Body)
	}
	w = ask("GET", "requests/req-gui", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "job-gui") {
		t.Fatalf("disabled recovery: %d %s", w.Code, w.Body)
	}
	mu.Lock()
	defer mu.Unlock()
	if cancels != 2 || posts != 1 {
		t.Fatalf("cancels=%d posts=%d", cancels, posts)
	}
}
