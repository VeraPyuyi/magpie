package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

// Seeing Done evaluated proves the worker reached its disabled wait, so
// enabling cannot race the initial config read or pass on an enabled boot.
type dotRefreshContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *dotRefreshContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestDotStatusRefreshAfterDisabledBoot(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dotbridge.SetCredentialStore(newDotMemStore())
	t.Cleanup(func() { dotbridge.SetCredentialStore(nil) })
	status := make(chan struct{}, 1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		json.NewEncoder(w).Encode(map[string]any{
			"version": 1, "enabled": true, "paired": true, "ready": true, "code": "ready",
			"verified_at": now.Add(-time.Second), "expires_at": now.Add(time.Minute),
			"subscription": map[string]any{"state": "active", "expires_at": now.Add(5 * time.Minute)},
			"capabilities": map[string]bool{"text": true, "tasks": false},
		})
		status <- struct{}{}
	}))
	defer up.Close()
	base, cancel := context.WithCancel(t.Context())
	ctx := &dotRefreshContext{Context: base, waiting: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		refreshDotStatus(ctx)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Dot refresher did not stop on cancellation")
		}
	}()
	select {
	case <-done:
		t.Fatal("Dot refresher permanently exited with disabled boot config")
	case <-ctx.waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("Dot refresher never reached disabled wait")
	}
	cfg := dotbridge.Config{Address: strings.TrimPrefix(up.URL, "http://"), Credential: "pair-ref"}
	if err := dotbridge.SavePairing(cfg, "paired-token"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-status:
	case <-done:
		t.Fatal("Dot refresher exited before status refresh")
	case <-time.After(15 * time.Second):
		t.Fatal("Dot refresher did not discover runtime enablement")
	}
}
