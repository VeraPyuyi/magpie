package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

func TestDotLegacyStableHeaderNeverReposts(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "same", true: "conflict"}[changed], func(t *testing.T) {
			h := newDotBridgeHarness()
			setupDotBarrierGateway(t, h)
			cfg, _ := dotbridge.LoadConfig()
			// Literal pre-generation persisted config and pre-generation wire identities.
			legacyConfig := map[string]any{"enabled": true, "address": cfg.Address, "credential": cfg.Credential, "answer_wait_seconds": 1}
			raw, _ := json.Marshal(legacyConfig)
			if err := os.WriteFile(filepath.Join(settings.Dir(), "dot-bridge.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
			oldScope := dotHashParts("local", callerOf(r).agent)
			oldID := "req-" + dotHashParts(oldScope, "retry-header")
			oldSession := "sess-" + dotHashParts(oldScope, oldID)
			body := []byte(`{"model":"dot/text","messages":[{"role":"user","content":"original"}]}`)
			sub, err := dotSubmission(provider.Chat, body, dotbridge.ModeAnswer, oldID, oldSession)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := dotbridge.Reserve(sub, oldScope); err != nil {
				t.Fatal(err)
			}
			conn, err := dotbridge.PairedConnection()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Status(t.Context()); err != nil {
				t.Fatal(err)
			}
			content := "original"
			if changed {
				content = "changed"
			}
			b := `{"model":"dot/text","messages":[{"role":"user","content":"` + content + `"}]}`
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(b))
			req.Header.Set(dotRequestHeader, "retry-header")
			w := httptest.NewRecorder()
			New().Handler().ServeHTTP(w, req)
			if h.gets.Load() != 0 {
				t.Fatal("legacy record queried against current pairing")
			}
			if h.posts.Load() != 0 {
				t.Fatalf("legacy stable retry upgraded into new POST: %d %s", h.posts.Load(), w.Body.String())
			}
			if !strings.Contains(w.Body.String(), oldID) {
				t.Fatalf("missing explicit original recovery identity %s: %s", oldID, w.Body.String())
			}
			if changed && !strings.Contains(w.Body.String(), "conflict") && !strings.Contains(w.Body.String(), "dot_original_fingerprint_required") {
				t.Fatalf("lost legacy content conflict: %s", w.Body.String())
			}
			rows, err := dotbridge.Requests(oldScope)
			if err != nil || len(rows) != 1 || !rows[0].BlockReplay {
				t.Fatalf("lost legacy barrier: %v %v", rows, err)
			}
		})
	}
}
