package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
)

// Inspect the real handler while its HTTP wait is still open. JSON inspection
// deliberately makes absence of the new observation a runtime assertion.
func TestDotLiveObservationBeforeHTTPDone(t *testing.T) {
	h := newDotBridgeHarness()
	setupDotBarrierGateway(t, h)
	s := New()
	done := make(chan struct{})
	go func() { defer close(done); dotHandlerCall(s, "visible", "hi", false) }()
	defer func() {
		<-done
		routes := s.Trace(t.Context(), 0, 0).Routes
		if len(routes) == 1 && routes[0].Tries[0].DotJob != nil && routes[0].Tries[0].DotJob.ObservedAt.After(routes[0].Tries[0].Start.Add(time.Duration(routes[0].Tries[0].Millis)*time.Millisecond)) {
			t.Error("final observation timestamp was regenerated after try ended")
		}
		if len(routes) != 1 || routes[0].Tries[0].DotJob == nil || routes[0].Tries[0].DotJob.State != "queued" {
			t.Errorf("final try lost pending job: %+v", routes)
		}
	}()
	deadline := time.After(3 * time.Second)
	for {
		routes := s.Trace(t.Context(), 0, 0).Routes
		if len(routes) > 0 && len(routes[0].Tries) > 0 {
			b, _ := json.Marshal(routes[0].Tries[0])
			var tr map[string]json.RawMessage
			json.Unmarshal(b, &tr)
			if h.posts.Load() > 0 && tr["dotJob"] != nil {
				var job map[string]any
				json.Unmarshal(tr["dotJob"], &job)
				if job["state"] != "queued" || routes[0].Tries[0].Done {
					t.Fatalf("not live: %s", b)
				}
				if h.posts.Load() != 1 || h.vendors.Load() != 0 {
					t.Fatal("unexpected dispatch")
				}
				return
			}
		}
		select {
		case <-done:
			t.Fatal("HTTP finished without publishing a live validated Dot job")
		case <-deadline:
			t.Fatal("no live observation")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestDotPermissionOmissionPrecedesShapeReason(t *testing.T) {
	h := newDotBridgeHarness()
	setupDotBarrierGateway(t, h)
	s := New()
	body := `{"model":"group/dotmix","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"fixture"}}]}`
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	r = r.WithContext(access.WithIdentity(r.Context(), access.Identity{Accounts: []string{"dot/forbidden"}}))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	routes := s.Trace(t.Context(), 0, 0).Routes
	if len(routes) != 1 {
		t.Fatalf("no route: %d %s", w.Code, w.Body)
	}
	found := false
	for _, item := range routes[0].DisplaySkippedDot {
		if item.Model == "text" {
			found = true
			if item.Reason != "not_authorized" {
				t.Fatalf("invented reason %+v", item)
			}
		}
	}
	if !found || h.posts.Load() != 0 {
		t.Fatalf("missing permission omission: %+v", routes[0].DisplaySkippedDot)
	}
}

func TestDotFinalTaskPreflightRecorded(t *testing.T) {
	h := newDotBridgeHarness()
	setupDotBarrierGateway(t, h)
	if err := provider.SaveGroup(provider.Group{ID: "task-final", Name: "task-final", Members: []string{"dot/task", "b/m"}, Routing: provider.Ordered}); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"group/task-final","messages":[{"role":"user","content":"task"},{"role":"assistant","content":"continuation"}]}`
	s := New()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || h.posts.Load() != 0 || h.vendors.Load() != 1 {
		t.Fatalf("dispatch %d posts=%d vendor=%d", w.Code, h.posts.Load(), h.vendors.Load())
	}
	routes := s.Trace(t.Context(), 0, 0).Routes
	if len(routes) != 1 || len(routes[0].DisplaySkippedDot) == 0 {
		t.Fatal("missing final mode skip")
	}
	found := false
	for _, item := range routes[0].DisplaySkippedDot {
		if item.Model == "task" && item.Reason == "task_prompt_required" {
			found = true
		}
	}
	if !found {
		t.Fatalf("wrong reason %+v", routes[0].DisplaySkippedDot)
	}
}

func TestDotLiveLifecycleImmutable(t *testing.T) {
	h := newDotBridgeHarness()
	setupDotBarrierGateway(t, h)
	cfg, _ := dotbridge.LoadConfig()
	cfg.AnswerWaitSeconds = 30
	if err := dotbridge.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := New()
	done := make(chan struct{})
	go func() { defer close(done); dotHandlerCall(s, "lifecycle", "hi", true) }()
	defer func() { <-done }()
	var prior *DotJobObservation
	for _, state := range []string{"queued", "running", "waiting_user", "completed"} {
		if state != "queued" {
			h.mu.Lock()
			for id, job := range h.jobs {
				job.State = state
				if state == "completed" {
					job.Result = &dotbridge.Result{Text: "literal result"}
				}
				h.jobs[id] = job
			}
			h.mu.Unlock()
		}
		deadline := time.After(10 * time.Second)
		for {
			routes := s.Trace(t.Context(), 0, 0).Routes
			if len(routes) > 0 && len(routes[0].Tries) > 0 {
				job := routes[0].Tries[0].DotJob
				if job != nil && job.State == state {
					if prior != nil && prior.State == state {
						t.Fatal("prior observation mutated")
					}
					prior = job
					break
				}
			}
			select {
			case <-deadline:
				t.Fatalf("missing live %s", state)
			default:
				time.Sleep(time.Millisecond)
			}
		}
	}
	if h.posts.Load() != 1 || h.vendors.Load() != 0 {
		t.Fatal("lifecycle redispatched")
	}
}

func TestDotObservationSafeErrorAndCompletedRetention(t *testing.T) {
	h := newDotBridgeHarness()
	h.state = dotbridge.StateCompleted
	h.text = "private result"
	setupDotBarrierGateway(t, h)
	s := New()
	dotHandlerCall(s, "final", "hi", false)
	routes := s.Trace(t.Context(), 0, 0).Routes
	if len(routes) != 1 || routes[0].Tries[0].DotJob == nil || routes[0].Tries[0].DotJob.State != dotbridge.StateCompleted {
		t.Fatalf("missing final completed observation: %+v", routes)
	}
	first := compactDotJob(dotbridge.Job{State: "running", ErrorCode: strings.Repeat("secret\n", 10000)})
	b, _ := json.Marshal(first)
	if strings.Contains(string(b), "secret") || first.ErrorCode != "dot_job_error" {
		t.Fatalf("unsafe trace error: %s", b)
	}
	second := compactDotJob(dotbridge.Job{State: "completed"})
	if first.State != "running" || second.State != "completed" {
		t.Fatal("observation mutated")
	}
}

func dotHandlerCallTools(s *Server, id, content string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]any{
		"model":    "group/dotmix",
		"messages": []any{map[string]any{"role": "user", "content": content}},
		"tools":    []any{map[string]any{"type": "function", "function": map[string]string{"name": "tool", "description": "x"}}},
	})
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(b)))
	if id != "" {
		r.Header.Set(dotRequestHeader, id)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestDotDisplaySkippedWhenCallerTools(t *testing.T) {
	h := newDotBridgeHarness()
	h.state = dotbridge.StateCompleted
	h.text = "vendor answer"
	setupDotBarrierGateway(t, h)
	s := New()
	w := dotHandlerCallTools(s, "", "hi")
	if h.posts.Load() != 0 {
		t.Fatalf("dot posted %d times", h.posts.Load())
	}
	if h.vendors.Load() != 1 || w.Code != 200 {
		t.Fatalf("vendor calls=%d code=%d %s", h.vendors.Load(), w.Code, w.Body)
	}
	routes := s.Trace(t.Context(), 0, 0).Routes
	if len(routes) != 1 || len(routes[0].DisplaySkippedDot) == 0 {
		t.Fatalf("skipped dot missing: %+v", routes)
	}
	if !strings.Contains(routes[0].DisplaySkippedDot[0].Reason, "tools") {
		t.Fatalf("reason %q", routes[0].DisplaySkippedDot[0].Reason)
	}
}
