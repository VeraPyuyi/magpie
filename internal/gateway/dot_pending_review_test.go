package gateway

import (
	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDotTaskPendingProgressProtocols(t *testing.T) {
	for _, proto := range []provider.Protocol{provider.Chat, provider.Responses} {
		for _, state := range []string{dotbridge.StateRunning, dotbridge.StateWaitingUser} {
			t.Run(string(proto)+state, func(t *testing.T) {
				h := newDotBridgeHarness()
				h.state = state
				setupDotBarrierGateway(t, h)
				cfg, err := dotbridge.LoadConfig()
				if err != nil {
					t.Fatal(err)
				}
				cfg.TaskWaitSeconds = 1
				if err = dotbridge.SaveConfig(cfg); err != nil {
					t.Fatal(err)
				}
				path, body := "/v1/chat/completions", `{"model":"dot/task","stream":true,"messages":[{"role":"user","content":"cloud task"}]}`
				if proto == provider.Responses {
					path, body = "/v1/responses", `{"model":"dot/task","stream":true,"input":"cloud task"}`
				}
				w := httptest.NewRecorder()
				New().Handler().ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
				for _, s := range []string{`"state":"` + state + `"`, `"mode":"task"`, `"status_url":"/v1/magpie/dot/requests/`} {
					if !strings.Contains(w.Body.String(), s) {
						t.Errorf("missing %s: %s", s, w.Body)
					}
				}
				if h.posts.Load() != 1 || h.vendors.Load() != 0 {
					t.Fatalf("posts=%d vendors=%d", h.posts.Load(), h.vendors.Load())
				}
				rows, err := dotbridge.RequestArchive()
				if err != nil || len(rows) != 1 {
					t.Fatalf("archive %v %v", rows, err)
				}
				get := httptest.NewRecorder()
				New().Handler().ServeHTTP(get, httptest.NewRequest("GET", "/v1/magpie/dot/requests/"+rows[0].RequestID, nil))
				if !strings.Contains(get.Body.String(), `"state":"`+state+`"`) || h.posts.Load() != 1 {
					t.Fatalf("GET progress: %s", get.Body)
				}
			})
		}
	}
}
