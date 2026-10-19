package dotbridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/settings"
)

func TestDotConsentAndCredentials(t *testing.T) {
	store := newMemStore()
	SetCredentialStore(store)
	t.Cleanup(func() { SetCredentialStore(newMemStore()) })

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfg := Config{
		Enabled:           true,
		Address:           "127.0.0.1:9",
		Credential:        "pair-ref-1",
		AnswerWaitSeconds: 120,
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	_, err := PairedConnection()
	if err != errNotPaired {
		t.Fatalf("missing vault creds must fail before POST: %v", err)
	}
	if err := savePairToken(cfg.Credential, "secret-token"); err != nil {
		t.Fatal(err)
	}
	conn, err := PairedConnection()
	if err != nil {
		t.Fatal(err)
	}
	// No server listening; Submit should fail on dial, not on missing token.
	attempt, err := NewAttempt(testSubmission())
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Submit(context.Background(), testSubmission(), attempt)
	if err == nil {
		t.Fatal("expected network error")
	}

	disabled := cfg
	disabled.Enabled = false
	if err := SaveConfig(disabled); err != nil {
		t.Fatal(err)
	}
	_, err = PairedConnection()
	if err != errDisabled {
		t.Fatalf("disabled config cannot enable itself: %v", err)
	}

	path := filepath.Join(settings.Dir(), "dot-bridge.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" || strings.Contains(string(b), "secret-token") {
		t.Fatal("config must store credential reference only")
	}
}
