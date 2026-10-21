package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/middleware"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

func TestDotOriginalUnsupportedCannotBeErased(t *testing.T) {
	for _, original := range []string{
		`"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"local"}}]`,
		`"messages":[{"role":"developer","content":"caller instructions"},{"role":"user","content":"hi"}]`,
		`"messages":[{"role":"user","content":"<cwd>/caller/repo</cwd>"}]`,
	} {
		t.Run(original, func(t *testing.T) {
			h := newDotBridgeHarness()
			h.state = dotbridge.StateCompleted
			h.text = "Dot must not answer"
			setupDotBarrierGateway(t, h)
			t.Cleanup(plugin.Settle)
			file := filepath.Join(t.TempDir(), "erase.middleware.js")
			if err := os.WriteFile(file, []byte(`export function onRequest(body) { delete body.tools; body.messages=[{role:"user",content:"plain"}]; return body; }`), 0600); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(plugin.List{Plugins: []plugin.Entry{{Spec: file}}})
			if err := os.WriteFile(filepath.Join(settings.Dir(), "plugins.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			middleware.Reload()
			defer middleware.Reload()
			r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"group/dotmix",`+original+`}`))
			w := httptest.NewRecorder()
			New().Handler().ServeHTTP(w, r)
			if err := provider.SaveGroup(provider.Group{ID: "task-mix", Name: "Task mix", Members: []string{"dot/task", "b/m"}, Routing: provider.Ordered}); err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{"group/task-mix", "dot/task", "dot/text"} {
				req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+target+`",`+original+`}`))
				out := httptest.NewRecorder()
				New().Handler().ServeHTTP(out, req)
				if target == "group/task-mix" && out.Code != 200 || target != "group/task-mix" && out.Code != 400 {
					t.Fatalf("target %s status=%d %s", target, out.Code, out.Body)
				}
			}
			if w.Code != 200 || h.posts.Load() != 0 || h.vendors.Load() != 2 {
				t.Fatalf("original erased: status=%d dot=%d vendor=%d %s", w.Code, h.posts.Load(), h.vendors.Load(), w.Body)
			}
		})
	}
}
