package dotbridge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDotRecoveryConnectionCannotReserveOrPOST(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	SetCredentialStore(newMemStore())
	t.Cleanup(func() { SetCredentialStore(nil) })
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(404) }))
	defer srv.Close()
	cfg := Config{Address: strings.TrimPrefix(srv.URL, "http://"), Credential: "recovery"}
	if err := SavePairing(cfg, "secret"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = LoadConfig()
	cfg.Enabled = false
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	_, c, err := RecoveryPairingSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	sub := Submission{RequestID: "req-new", SessionID: "session-new", Mode: ModeAnswer, Messages: []Message{{Role: "user", Content: "hello"}}}
	a, _ := NewAttempt(sub)
	if _, err := c.Enqueue(t.Context(), sub, "cli:"+c.PairingIdentity(), a); err == nil {
		t.Fatal("recovery connection admitted new work")
	}
	if _, err := c.Submit(t.Context(), sub, a); err == nil {
		t.Fatal("recovery connection submitted")
	}
	if calls.Load() != 0 {
		t.Fatal("recovery connection sent POST")
	}
	items, err := Requests("cli:" + c.PairingIdentity())
	if err != nil || len(items) != 0 {
		t.Fatalf("reserved new identity: %+v %v", items, err)
	}
}
