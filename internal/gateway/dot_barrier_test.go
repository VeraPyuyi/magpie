package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
)

type dotBridgeHarness struct {
	posts, gets, vendors atomic.Int32
	mu                   sync.Mutex
	jobs                 map[string]dotbridge.Job
	state                string
	text                 string
	drop                 bool
	invalidReceipt       bool
	cancels              atomic.Int32
	onSubmit             func(*http.Request)
}

func newDotBridgeHarness() *dotBridgeHarness {
	return &dotBridgeHarness{jobs: map[string]dotbridge.Job{}, state: dotbridge.StateQueued}
}
func (h *dotBridgeHarness) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			h.cancels.Add(1)
			w.WriteHeader(200)
		case r.URL.Path == "/dot-bridge/v1/status":
			now := time.Now().UTC()
			json.NewEncoder(w).Encode(map[string]any{"version": 1, "enabled": true, "paired": true, "ready": true, "code": "ready", "verified_at": now.Add(-time.Second), "expires_at": now.Add(time.Minute), "subscription": map[string]any{"state": "active", "expires_at": now.Add(time.Minute)}, "capabilities": map[string]bool{"text": true}})
		case r.Method == "POST" && r.URL.Path == "/dot-bridge/v1/jobs":
			h.posts.Add(1)
			if h.onSubmit != nil {
				h.onSubmit(r)
			}
			var sub dotbridge.Submission
			json.NewDecoder(r.Body).Decode(&sub)
			digest, _ := dotbridge.Digest(sub)
			job := dotbridge.Job{Version: 1, JobID: "job-" + sub.RequestID, RequestID: sub.RequestID, RequestDigest: digest, Mode: sub.Mode, SessionID: sub.SessionID, State: h.state}
			if h.state == dotbridge.StateCompleted {
				job.Result = &dotbridge.Result{Text: h.text}
			}
			h.mu.Lock()
			h.jobs[sub.RequestID] = job
			h.mu.Unlock()
			if h.drop {
				c, _, _ := w.(http.Hijacker).Hijack()
				c.Close()
				return
			}
			w.WriteHeader(202)
			if h.invalidReceipt {
				w.Write([]byte(`{"accepted":true,"version":1,"job_id":"job-invalid","request_digest":"wrong"}`))
				return
			}
			json.NewEncoder(w).Encode(struct {
				dotbridge.Job
				Accepted bool `json:"accepted"`
			}{job, true})
		case strings.HasPrefix(r.URL.Path, "/dot-bridge/v1/requests/"):
			h.gets.Add(1)
			id := strings.TrimPrefix(r.URL.Path, "/dot-bridge/v1/requests/")
			h.mu.Lock()
			job := h.jobs[id]
			h.mu.Unlock()
			if job.JobID == "" {
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(job)
		default:
			w.WriteHeader(404)
		}
	}))
}
func setupDotBarrierGateway(t *testing.T, h *dotBridgeHarness) {
	t.Helper()
	noRests()
	t.Cleanup(noRests)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dotbridge.SetJournalPath(filepath.Join(t.TempDir(), "requests.json"))
	dotbridge.SetCredentialStore(newDotMemStore())
	t.Cleanup(func() { dotbridge.SetJournalPath(""); dotbridge.SetCredentialStore(nil) })
	srv := h.serve()
	t.Cleanup(srv.Close)
	cfg := dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(srv.URL, "http://"), Credential: "fixture", AnswerWaitSeconds: 1}
	if err := dotbridge.SavePairing(cfg, "paired-token"); err != nil {
		t.Fatal(err)
	}
	conn, err := dotbridge.PairedConnection()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Status(t.Context()); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.vendors.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(chatWholeOK))
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "b", Name: "B", Key: "k", Models: []string{"m"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{ID: "inner", Name: "inner", Members: []string{"dot/text", "b/m"}, Routing: provider.Ordered, FirstToken: 1}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{ID: "dotmix", Name: "dotmix", Members: []string{"group/inner", "b/m"}, Routing: provider.Ordered, FirstToken: 1}); err != nil {
		t.Fatal(err)
	}
}
func dotHandlerCall(s *Server, id, content string, stream bool) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]any{"model": "group/dotmix", "stream": stream, "messages": []any{map[string]any{"role": "user", "content": content}}})
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(b)))
	if id != "" {
		r.Header.Set(dotRequestHeader, id)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestDotAcceptedStopsEveryReplay(t *testing.T) {
	for _, state := range []string{dotbridge.StateQueued, dotbridge.StateFailed, dotbridge.StateOutcomeUnknown, dotbridge.StateCancelRequested, dotbridge.StateCompleted} {
		t.Run(state, func(t *testing.T) {
			h := newDotBridgeHarness()
			h.state = state
			setupDotBarrierGateway(t, h)
			s := New()
			w := dotHandlerCall(s, "", "hi", false)
			if h.posts.Load() != 1 || h.vendors.Load() != 0 || w.Code != 409 {
				t.Fatalf("posts=%d vendors=%d code=%d %s", h.posts.Load(), h.vendors.Load(), w.Code, w.Body)
			}
			routes := s.Trace(t.Context(), 0, 0).Routes
			if len(routes) != 1 || len(routes[0].Tries) != 1 || routes[0].Tries[0].Dot == nil || !routes[0].Tries[0].Dot.BlockReplay {
				t.Fatalf("routes=%+v", routes)
			}
			if len(s.Recent()) != 1 || !strings.Contains(w.Body.String(), `"automatic_redispatch":false`) {
				t.Fatalf("bookkeeping/error %s", w.Body)
			}
		})
	}
}
func TestDotUnknownStopsEveryReplay(t *testing.T) {
	h := newDotBridgeHarness()
	h.drop = true
	setupDotBarrierGateway(t, h)
	w := dotHandlerCall(New(), "stable", "hi", false)
	if h.posts.Load() != 1 || h.vendors.Load() != 0 || !strings.Contains(w.Body.String(), dotErrUnknown) {
		t.Fatalf("posts=%d vendors=%d %s", h.posts.Load(), h.vendors.Load(), w.Body)
	}
	w = dotHandlerCall(New(), "stable", "hi", false)
	if h.posts.Load() != 1 || h.gets.Load() != 1 || h.vendors.Load() != 0 {
		t.Fatalf("recovery posts=%d gets=%d vendors=%d %s", h.posts.Load(), h.gets.Load(), h.vendors.Load(), w.Body)
	}
}
func TestDotClientRetryIdentity(t *testing.T) {
	h := newDotBridgeHarness()
	h.state = dotbridge.StateCompleted
	h.text = "verified answer"
	setupDotBarrierGateway(t, h)
	for i := 0; i < 2; i++ {
		w := dotHandlerCall(New(), "stable", "hi", false)
		if w.Code != 200 || !strings.Contains(w.Body.String(), h.text) {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	if h.posts.Load() != 1 || h.gets.Load() != 1 {
		t.Fatalf("posts=%d gets=%d", h.posts.Load(), h.gets.Load())
	}
	w := dotHandlerCall(New(), "stable", "changed", false)
	if h.posts.Load() != 1 || w.Code < 400 {
		t.Fatalf("conflict posts=%d code=%d", h.posts.Load(), w.Code)
	}
}
func TestDotWaitHasNoFakeContent(t *testing.T) {
	h := newDotBridgeHarness()
	setupDotBarrierGateway(t, h)
	w := dotHandlerCall(New(), "", "hi", true)
	if h.posts.Load() != 1 || h.vendors.Load() != 0 {
		t.Fatalf("posts=%d vendors=%d", h.posts.Load(), h.vendors.Load())
	}
	b := w.Body.String()
	for _, bad := range []string{`"choices"`, `"usage"`, `response.created`, `response.completed`} {
		if strings.Contains(b, bad) {
			t.Fatalf("fake output %s", b)
		}
	}
	if !strings.Contains(b, dotErrPending) {
		t.Fatal(b)
	}
}
func TestDotRoleAndSessionBoundaries(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	r.Header.Set(dotRequestHeader, "stable")
	a, _ := dotRequestID(r)
	b, _ := dotRequestID(r)
	if a != b {
		t.Fatal("unstable request")
	}
	r.Header.Set(SessionHeader, "one")
	one := dotSessionID(r, a)
	r.Header.Set(SessionHeader, "two")
	if one == dotSessionID(r, a) {
		t.Fatal("sessions joined")
	}
	// Arbitrary loopback auth text is not an authenticated identity.
	r.Header.Set("Authorization", "Bearer other")
	c, _ := dotRequestID(r)
	if c != a {
		t.Fatal("raw auth changed local scope")
	}
	r.Header.Del(dotRequestHeader)
	a, _ = dotRequestID(r)
	b, _ = dotRequestID(r)
	if a == b {
		t.Fatal("anonymous requests joined")
	}
}

func TestDotInvalidReceiptExactlyOnePOST(t *testing.T) {
	h := newDotBridgeHarness()
	h.invalidReceipt = true
	setupDotBarrierGateway(t, h)
	s := New()
	w := dotHandlerCall(s, "", "hi", true)
	if h.posts.Load() != 1 || h.vendors.Load() != 0 {
		t.Fatalf("POSTs=%d vendors=%d body=%s", h.posts.Load(), h.vendors.Load(), w.Body)
	}
	if !strings.Contains(w.Body.String(), dotErrUnknown) {
		t.Fatal(w.Body.String())
	}
	rs := s.Trace(t.Context(), 0, 0).Routes
	if len(rs) != 1 || len(rs[0].Tries) != 1 || rs[0].Tries[0].Dot == nil || !rs[0].Tries[0].Dot.BlockReplay {
		t.Fatalf("routes=%+v", rs)
	}
}

func TestDotVendorCannotSetAcceptance(t *testing.T) {
	h := newDotBridgeHarness()
	setupDotBarrierGateway(t, h)
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Dot-Accepted", "true")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		w.Write([]byte(`{"accepted":true,"job_id":"spoof","error":{"message":"busy"}}`))
	}))
	t.Cleanup(a.Close)
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Key: "k", Models: []string{"m"}, Chat: a.URL + "/v1", Fallback: []string{"b/m"}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"a/m","messages":[{"role":"user","content":"hi"}]}`))
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || h.vendors.Load() != 1 || h.posts.Load() != 0 {
		t.Fatalf("code=%d vendors=%d posts=%d %s", w.Code, h.vendors.Load(), h.posts.Load(), w.Body)
	}
	for _, route := range s.Trace(t.Context(), 0, 0).Routes {
		for _, tr := range route.Tries {
			if tr.Dot != nil {
				t.Fatal("vendor forged Dot state")
			}
		}
	}
}
func TestDotPreSubmitFailureCanFallback(t *testing.T) {
	h := newDotBridgeHarness()
	setupDotBarrierGateway(t, h)
	// A corrupt journal must fail before POST; the normal provider can answer.
	journal := filepath.Join(t.TempDir(), "requests.json")
	if err := os.WriteFile(journal, []byte(`broken`), 0600); err != nil {
		t.Fatal(err)
	}
	dotbridge.SetJournalPath(journal)
	w := dotHandlerCall(New(), "", "hi", false)
	if h.posts.Load() != 0 || h.vendors.Load() != 1 || w.Code != 200 {
		t.Fatalf("posts=%d vendors=%d code=%d %s", h.posts.Load(), h.vendors.Load(), w.Code, w.Body)
	}
}
