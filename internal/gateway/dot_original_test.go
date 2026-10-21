package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/middleware"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/settings"
)

func TestDotOriginalRequestSurvivesLossyMiddleware(t *testing.T) {
	h := newDotBridgeHarness()
	h.state = dotbridge.StateCompleted
	h.text = "original result"
	setupDotBarrierGateway(t, h)
	t.Cleanup(plugin.Settle)
	file := filepath.Join(t.TempDir(), "lossy.middleware.js")
	if err := os.WriteFile(file, []byte(`export function onRequest(body) { body.messages = [{role:"user",content:"fixed message"}]; return body; }`), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(plugin.List{Plugins: []plugin.Entry{{Spec: file}}})
	if err := os.WriteFile(filepath.Join(settings.Dir(), "plugins.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	middleware.Reload()
	defer middleware.Reload()
	s := New()
	if w := dotHandlerCall(s, "raw-identity", "A", false); w.Code != 200 {
		t.Fatalf("first %d %s", w.Code, w.Body)
	}
	if w := dotHandlerCall(s, "raw-identity", "B", false); w.Code != 409 {
		t.Fatalf("different original body recovered old result: %d %s", w.Code, w.Body)
	}
	if h.posts.Load() != 1 || h.vendors.Load() != 0 {
		t.Fatalf("replayed posts=%d vendors=%d", h.posts.Load(), h.vendors.Load())
	}
	if w := dotHandlerCall(s, "raw-identity", "A", false); w.Code != 200 {
		t.Fatalf("identical original %d %s", w.Code, w.Body)
	}
	if w := dotHandlerCall(s, "raw-identity", "A ", false); w.Code != 409 {
		t.Fatalf("exact original bytes not bound: %d %s", w.Code, w.Body)
	}
	archive, err := dotbridge.RequestArchive()
	if err != nil || len(archive) != 1 || len(archive[0].OriginalFingerprint) != 64 {
		t.Fatalf("original fingerprint missing: %+v %v", archive, err)
	}
}
