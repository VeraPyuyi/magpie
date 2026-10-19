package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/usage"
)

func TestDotReviewNestedWireFields(t *testing.T) {
	for _, extra := range []string{`"tool_calls":[]`, `"function_call":{"name":"read"}`, `"tool_call_id":"c"`, `"name":"actor"`, `"surprise":true`} {
		t.Run(extra, func(t *testing.T) {
			h := newDotBridgeHarness()
			setupDotBarrierGateway(t, h)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"dot/text","messages":[{"role":"assistant","content":"hi",`+extra+`}]}`))
			New().Handler().ServeHTTP(w, r)
			if w.Code != 400 || h.posts.Load() != 0 {
				t.Fatalf("code=%d posts=%d %s", w.Code, h.posts.Load(), w.Body)
			}
		})
	}
	for _, body := range []string{
		`{"model":"dot/text","input":[{"type":"message","role":"user","content":"hi","surprise":true}]}`,
		`{"model":"dot/text","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi","image":"hidden"}]}]}`,
		`{"model":"dot/text","input":"hi","service_tier":"priority"}`,
	} {
		req, err := dotRequirementsOf(provider.Responses, []byte(body))
		if err == nil && req.ok() {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestDotReviewAuthenticatedScopes(t *testing.T) {
	h := newDotBridgeHarness()
	h.state = dotbridge.StateCompleted
	h.text = "answer"
	setupDotBarrierGateway(t, h)
	cfg := settings.Load()
	cfg.LAN = true
	if err := settings.Save(cfg); err != nil {
		t.Fatal(err)
	}
	a, err := access.Update("add-key", access.Change{Name: "one"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := access.Update("add-key", access.Change{Name: "two"})
	if err != nil {
		t.Fatal(err)
	}
	s := New()
	for _, key := range []string{a, b, a} {
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"dot/text","messages":[{"role":"user","content":"hi"}]}`))
		r.RemoteAddr = "192.168.1.9:5000"
		r.Header.Set("Authorization", "Bearer stale")
		r.Header.Set("x-api-key", key)
		r.Header.Set(dotRequestHeader, "same")
		w := httptest.NewRecorder()
		lanGuard(s.Handler()).ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	if h.posts.Load() != 2 || h.gets.Load() != 1 {
		t.Fatalf("cross-caller identity: posts=%d gets=%d", h.posts.Load(), h.gets.Load())
	}
}

func TestDotReviewFailureLedger(t *testing.T) {
	for _, state := range []string{dotbridge.StateQueued, dotbridge.StateFailed, "invalid-receipt"} {
		t.Run(state, func(t *testing.T) {
			h := newDotBridgeHarness()
			if state == "invalid-receipt" {
				h.invalidReceipt = true
			} else {
				h.state = state
			}
			setupDotBarrierGateway(t, h)
			s := New()
			w := dotHandlerCall(s, "", "hi", false)
			rows := usage.Load(time.Time{})
			found := false
			for _, row := range rows {
				if row.Provider == "dot" {
					found = true
					if row.Status != 409 || row.Error == "" || row.Input != 0 || row.Output != 0 {
						t.Fatalf("row=%+v", row)
					}
				}
			}
			if !found {
				t.Fatalf("no Dot ledger row: code=%d rows=%+v", w.Code, rows)
			}
		})
	}
}

func TestDotReviewCompletedStreams(t *testing.T) {
	oldWatch, oldKeep := watchEvery, keepaliveEvery
	watchEvery = time.Microsecond
	keepaliveEvery = 0
	t.Cleanup(func() { watchEvery, keepaliveEvery = oldWatch, oldKeep })
	for _, proto := range []provider.Protocol{provider.Chat, provider.Responses} {
		t.Run(string(proto), func(t *testing.T) {
			h := newDotBridgeHarness()
			h.state = dotbridge.StateCompleted
			h.text = "verified result"
			setupDotBarrierGateway(t, h)
			for i := 0; i < 20; i++ {
				s := New()
				w := httptest.NewRecorder()
				path, body := "/v1/chat/completions", `{"model":"dot/text","stream":true,"messages":[{"role":"user","content":"hi"}]}`
				if proto == provider.Responses {
					path, body = "/v1/responses", `{"model":"dot/text","stream":true,"input":"hi"}`
				}
				r := httptest.NewRequest("POST", path, strings.NewReader(body))
				s.Handler().ServeHTTP(w, r)
				if w.Code != 200 || !strings.Contains(w.Body.String(), h.text) || strings.Contains(w.Body.String(), `"usage"`) {
					t.Fatalf("%d %s", w.Code, w.Body)
				}
				done := "[DONE]"
				prefix := "chatcmpl-"
				if proto == provider.Responses {
					done = "event: response.completed"
					prefix = "resp_"
				}
				if strings.Count(w.Body.String(), done) != 1 {
					t.Fatalf("finish count: %s", w.Body)
				}
				recent := s.Recent()
				if len(recent) != 1 || !strings.HasPrefix(recent[0].Usage.ResponseID, prefix) {
					t.Errorf("response id mismatch")
					continue
				}
				rows := usage.Load(time.Time{})
				if len(rows) == 0 || rows[len(rows)-1].ResponseID != recent[0].Usage.ResponseID {
					t.Fatalf("ledger response ID mismatch: %+v", rows)
				}
				if !strings.Contains(w.Body.String(), `"id":"`+recent[0].Usage.ResponseID+`"`) {
					t.Fatalf("wire id differs: %s", w.Body)
				}
			}
		})
	}
}

// The writer's Flush notification pins the ordering: keepalive must reach the
// client before the fake POST returns. No sleeps or wall-clock guesses.
type dotFlushSignal struct {
	*httptest.ResponseRecorder
	once sync.Once
	sent chan struct{}
}

func (w *dotFlushSignal) Flush() { w.ResponseRecorder.Flush(); w.once.Do(func() { close(w.sent) }) }
func TestDotReviewKeepaliveErrorAndCancel(t *testing.T) {
	oldWatch, oldKeep := watchEvery, keepaliveEvery
	watchEvery = time.Millisecond
	keepaliveEvery = 0
	t.Cleanup(func() { watchEvery, keepaliveEvery = oldWatch, oldKeep })
	for _, proto := range []provider.Protocol{provider.Chat, provider.Responses} {
		t.Run(string(proto), func(t *testing.T) {
			h := newDotBridgeHarness()
			setupDotBarrierGateway(t, h)
			w := &dotFlushSignal{ResponseRecorder: httptest.NewRecorder(), sent: make(chan struct{})}
			h.onSubmit = func(r *http.Request) {
				select {
				case <-w.sent:
				case <-r.Context().Done():
				}
			}
			s := New()
			path, body := "/v1/chat/completions", `{"model":"dot/text","stream":true,"messages":[{"role":"user","content":"hi"}]}`
			if proto == provider.Responses {
				path, body = "/v1/responses", `{"model":"dot/text","stream":true,"input":"hi"}`
			}
			r := httptest.NewRequest("POST", path, strings.NewReader(body))
			s.Handler().ServeHTTP(w, r)
			select {
			case <-w.sent:
			default:
				t.Fatal("no keepalive")
			}
			if w.Code != 200 || !strings.Contains(w.Body.String(), ": keepalive") || !strings.Contains(w.Body.String(), dotErrPending) || strings.Contains(w.Body.String(), "[DONE]") || strings.Contains(w.Body.String(), "response.completed") {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			route := s.Trace(t.Context(), 0, 0).Routes[0]
			tr := route.Tries[0]
			if tr.Status != 200 || tr.Fail != failOther || tr.DotError != dotErrPending {
				t.Fatalf("wire/semantic failure: %+v", tr)
			}
			rows := usage.Load(time.Time{})
			if len(rows) == 0 || rows[len(rows)-1].Status != 200 || rows[len(rows)-1].Error == "" {
				t.Fatalf("stream ledger=%+v", rows)
			}
			if proto == provider.Responses && !strings.Contains(w.Body.String(), "event: error") {
				t.Fatal(w.Body.String())
			}
		})
	}
}

func TestDotReviewDefaultBudget(t *testing.T) {
	h := newDotBridgeHarness()
	setupDotBarrierGateway(t, h)
	cfg, err := dotbridge.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AnswerWaitSeconds = 0
	if err = dotbridge.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err = dotbridge.LoadConfig()
	if err != nil || cfg.AnswerWaitSeconds != 120 {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	for _, v := range []int{-1, 601} {
		cfg.AnswerWaitSeconds = v
		if dotbridge.SaveConfig(cfg) == nil {
			t.Fatalf("accepted budget %d", v)
		}
	}
}

// First header access by an answer holds keepAlive's map iteration in flight.
// The scheduling wrapper delegates all writes to the real holdWriter.
type dotRaceWriter struct {
	*holdWriter
	once sync.Once
	stop chan struct{}
	over chan struct{}
}

func (w *dotRaceWriter) Header() http.Header {
	w.once.Do(func() {
		go func() {
			defer close(w.over)
			for {
				select {
				case <-w.stop:
					return
				default:
				}
				w.mu.Lock()
				w.alive.sent = false
				w.keepQuiet()
				w.mu.Unlock()
			}
		}()
	})
	runtime.Gosched()
	return w.holdWriter.Header()
}
func TestDotReviewConcurrentHeader(t *testing.T) {
	h := newDotBridgeHarness()
	h.state = dotbridge.StateCompleted
	h.text = "answer"
	setupDotBarrierGateway(t, h)
	old := keepaliveEvery
	keepaliveEvery = 0
	t.Cleanup(func() { keepaliveEvery = old })
	for _, proto := range []provider.Protocol{provider.Chat, provider.Responses} {
		body := []byte(`{"model":"dot/text","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		if proto == provider.Responses {
			body = []byte(`{"model":"dot/text","stream":true,"input":"hi"}`)
		}
		r, err := prepareDot(httptest.NewRequest("POST", "/v1/chat/completions", nil), proto, body)
		if err != nil {
			t.Fatal(err)
		}
		hw := newHoldWriter(httptest.NewRecorder(), true)
		hw.ctx = r.Context()
		hw.streams = true
		hw.alive = &keptAlive{proto: proto}
		hw.status = 200
		hw.stream = true
		for i := 0; i < 10000; i++ {
			hw.header.Set(fmt.Sprintf("X-Fill-%d", i), "value")
		}
		w := &dotRaceWriter{holdWriter: hw, stop: make(chan struct{}), over: make(chan struct{})}
		call := Call{}
		_ = w.Header() // Start keepQuiet before completion, on both old and fixed code.
		New().serveDot(w, r, proto, provider.Provider{}, "text", body, &call)
		w.once.Do(func() { close(w.over) })
		close(w.stop)
		<-w.over
	}
}

func TestDotReviewClientDisconnectNoCancel(t *testing.T) {
	h := newDotBridgeHarness()
	setupDotBarrierGateway(t, h)
	cfg, _ := dotbridge.LoadConfig()
	cfg.AnswerWaitSeconds = 0
	if err := dotbridge.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	submitted := make(chan struct{})
	h.onSubmit = func(r *http.Request) { close(submitted); cancel() }
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"group/dotmix","stream":true,"messages":[{"role":"user","content":"hi"}]}`)).WithContext(ctx)
	w := httptest.NewRecorder()
	s := New()
	s.Handler().ServeHTTP(w, r)
	select {
	case <-submitted:
	default:
		t.Fatal("never submitted")
	}
	if h.posts.Load() != 1 || h.cancels.Load() != 0 || h.vendors.Load() != 0 {
		t.Fatalf("posts=%d cancel=%d vendor=%d", h.posts.Load(), h.cancels.Load(), h.vendors.Load())
	}
	rs := s.Trace(t.Context(), 0, 0).Routes
	if len(rs) != 1 || rs[0].Tries[0].Fail != failCanceled {
		t.Fatalf("routes=%+v", rs)
	}
}

func TestDotReviewEarlierFailureAndPatience(t *testing.T) {
	h := newDotBridgeHarness()
	h.invalidReceipt = true
	setupDotBarrierGateway(t, h)
	var before atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		before.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		w.Write([]byte(`{"error":{"message":"earlier busy"}}`))
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "prior", Name: "Prior", Key: "k", Models: []string{"m"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{ID: "dotmix", Name: "dotmix", Members: []string{"prior/m", "group/inner", "b/m"}, Routing: provider.Ordered, Patience: 60, FirstToken: 1}); err != nil {
		t.Fatal(err)
	}
	s := New()
	w := dotHandlerCall(s, "", "hi", false)
	if before.Load() != 1 || h.posts.Load() != 1 || h.vendors.Load() != 0 || !strings.Contains(w.Body.String(), dotErrUnknown) || strings.Contains(w.Body.String(), "earlier busy") {
		t.Fatalf("prior=%d posts=%d vendor=%d %s", before.Load(), h.posts.Load(), h.vendors.Load(), w.Body)
	}
	rs := s.Trace(t.Context(), 0, 0).Routes
	if len(rs) != 1 || len(rs[0].Tries) != 2 || rs[0].Tries[1].Again != 0 || rs[0].Tries[1].Reset != nil || rs[0].Tries[1].Replan != 0 {
		t.Fatalf("replayed %+v", rs)
	}
}

func TestDotReviewDoesNotRescueSmallMember(t *testing.T) {
	h := newDotBridgeHarness()
	h.state = dotbridge.StateFailed
	setupDotBarrierGateway(t, h)
	small := &scripted{replies: []reply{{200, "", chatOK}}}
	roomyMember(t, "small", 2000, small)
	if err := provider.SaveGroup(provider.Group{ID: "dotmix", Name: "dotmix", Members: []string{"dot/text", "small/m"}, Routing: provider.Ordered, Patience: 60}); err != nil {
		t.Fatal(err)
	}
	s := New()
	w := dotHandlerCall(s, "", strings.Repeat("word ", 1760), false)
	if w.Code != 409 || h.posts.Load() != 1 || small.n != 0 {
		t.Fatalf("code=%d post=%d small=%d %s", w.Code, h.posts.Load(), small.n, w.Body)
	}
	if len(lastRoute(s).Tries) != 1 {
		t.Fatalf("tries %+v", lastRoute(s).Tries)
	}
}
