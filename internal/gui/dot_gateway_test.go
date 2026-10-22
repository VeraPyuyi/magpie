package gui

import (
	"encoding/json"
	"github.com/yetone/magpie/internal/dotbridge"
	"path/filepath"
	"strings"
	"testing"
)

func TestDotGatewayDisplayScopeAndPrivacy(t *testing.T) {
	dotbridge.SetJournalPath(filepath.Join(t.TempDir(), "journal.json"))
	t.Cleanup(func() { dotbridge.SetJournalPath("") })
	cfg := dotbridge.Config{Address: "127.0.0.1:1234", Credential: "fixture", Enabled: true}
	// SaveConfig assigns pairing identity in the same way production pairing does.
	if err := dotbridge.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg, _ = dotbridge.LoadConfig()
	identity := dotbridge.PairingIdentity(cfg)
	if identity == "" {
		t.Fatal("fixture identity missing")
	}
	sub := dotbridge.Submission{RequestID: "visible", SessionID: "session", Mode: "task", Task: "private task"}
	for _, scope := range []string{identity + ":" + strings.Repeat("a", 64), identity + ":not-a-gateway", "gui:" + identity, "cli:" + identity} {
		if _, _, err := dotbridge.Reserve(sub, scope); err != nil {
			t.Fatal(err)
		}
	}
	items, err := dotGatewaySnapshots(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(items)
	if len(items) != 1 {
		t.Fatalf("only recognized gateway scopes: %s", b)
	}
	for _, secret := range []string{"digest", "original_fingerprint", "private task", "scope"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("display leaked %s: %s", secret, b)
		}
	}
}
