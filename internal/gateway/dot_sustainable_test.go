package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
)

func TestDotStableRecoveryBeforeResolution(t *testing.T) {
	for _, state := range []string{"unready", "replacement", "disabled", "unpaired"} {
		t.Run(state, func(t *testing.T) {
			replacement := state == "replacement" || state == "unpaired"
			h := newDotBridgeHarness()
			h.state = dotbridge.StateCompleted
			h.text = "original answer"
			setupDotBarrierGateway(t, h)
			s := New()
			first := dotHandlerCall(s, "stable-recovery", "hello", false)
			if first.Code != 200 {
				t.Fatalf("first: %d %s", first.Code, first.Body)
			}
			cfg, _ := dotbridge.LoadConfig()
			if state == "unpaired" {
				if err := dotbridge.ClearPairing(cfg); err != nil {
					t.Fatal(err)
				}
			} else if replacement {
				if err := dotbridge.SavePairing(cfg, "replacement"); err != nil {
					t.Fatal(err)
				}
			} else {
				if state == "disabled" {
					cfg.Enabled = false
				}
				if err := dotbridge.SaveConfig(cfg); err != nil {
					t.Fatal(err)
				}
			}
			// Reorder the group while Dot's readiness cache is invalidated.
			if err := provider.SaveGroup(provider.Group{ID: "dotmix", Name: "dotmix", Members: []string{"b/m", "dot/text"}, Routing: provider.Ordered}); err != nil {
				t.Fatal(err)
			}
			next := dotHandlerCall(s, "stable-recovery", "hello", false)
			if h.posts.Load() != 1 || h.vendors.Load() != 0 {
				t.Fatalf("replayed: posts=%d vendors=%d %s", h.posts.Load(), h.vendors.Load(), next.Body)
			}
			if !replacement && (next.Code != 200 || !strings.Contains(next.Body.String(), "original answer")) {
				t.Fatalf("recovery: %d %s", next.Code, next.Body)
			}
			if replacement && next.Code != 409 {
				t.Fatalf("replacement must require original authorization: %d %s", next.Code, next.Body)
			}
		})
	}
}

func TestDotUnavailableGroupNoPOST(t *testing.T) {
	h := newDotBridgeHarness()
	setupDotBarrierGateway(t, h)
	h.unready.Store(true)
	conn, _ := dotbridge.PairedConnection()
	_, _ = conn.Status(t.Context())
	w := dotHandlerCall(New(), "new-identity", "hello", false)
	if h.posts.Load() != 0 {
		t.Fatalf("unavailable Dot submitted: %d %s", w.Code, w.Body)
	}
}

func TestDotExplicitRecoveryWhenModelsHidden(t *testing.T) {
	h := newDotBridgeHarness()
	h.state = dotbridge.StateCompleted
	h.text = "same job"
	setupDotBarrierGateway(t, h)
	s := New()
	ask := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"dot/text","messages":[{"role":"user","content":"hello"}]}`))
		r.Header.Set(dotRequestHeader, "stable")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := ask(); w.Code != 200 {
		t.Fatalf("first %d %s", w.Code, w.Body)
	}
	h.expired.Store(true)
	conn, _ := dotbridge.PairedConnection()
	_, _ = conn.Status(t.Context())
	if _, _, ok := provider.Resolve("dot/text"); ok {
		t.Fatal("expired model still executable")
	}
	if w := ask(); w.Code != 200 || !strings.Contains(w.Body.String(), "same job") {
		t.Fatalf("hidden recovery %d %s", w.Code, w.Body)
	}
	if h.posts.Load() != 1 || h.gets.Load() != 1 {
		t.Fatalf("posts=%d gets=%d", h.posts.Load(), h.gets.Load())
	}
}
