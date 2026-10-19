//go:build !windows

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/settings"
)

// Submit the actual same-root files against one sandbox/store. Convenience
// helpers such as dotPairAccepted install their own sandbox and are therefore
// deliberately absent from this mode-only control.
func TestDotFixtureModeDecidesTheOutcome(t *testing.T) {
	store := dotSandbox(t)
	root := dotTempDir(t)
	const token = "sentinel-private-token"
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/dot-bridge/v1/status" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("unexpected pairing status request")
		}
		dotStatusPaired(w)
	}))
	defer srv.Close()
	listen := strings.TrimPrefix(srv.URL, "http://")
	body, err := json.Marshal(dotPairingFile{
		Version: 1, ClientID: "operator", QueueID: "operator",
		Token: token, Listen: listen, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	leftPath := filepath.Join(root, "mode-left.json")
	rightPath := filepath.Join(root, "mode-right.json")
	dotWriteFile(t, leftPath, body)
	dotWriteFile(t, rightPath, body)
	dotChmod(t, leftPath, 0600)
	dotChmod(t, rightPath, 0644)
	configPath := filepath.Join(settings.Dir(), "dot-bridge.json")

	// First reject the actual broad file with no prior import masking writes.
	var out bytes.Buffer
	err = dotTo(context.Background(), &out, []string{"pair", rightPath})
	if err == nil || out.Len() != 0 || store.writes != 0 || calls.Load() != 0 || store.token != "fixture-secret" {
		t.Fatalf("0644 import before pairing: err=%v output=%q writes=%d status=%d", err, out.String(), store.writes, calls.Load())
	}
	if strings.Contains(err.Error(), token) {
		t.Fatal("rejection exposed pairing token")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("0644 import created configuration: %v", err)
	}

	// Import the actual private file, using the same installed fake store.
	if err := dotTo(context.Background(), &out, []string{"pair", leftPath}); err != nil {
		t.Fatalf("0600 import: %v", err)
	}
	if store.writes != 1 || store.token != token || calls.Load() != 1 || out.Len() == 0 || strings.Contains(out.String(), token) {
		t.Fatalf("0600 import: writes=%d status=%d output=%q", store.writes, calls.Load(), out.String())
	}
	cfg, err := dotbridge.LoadConfig()
	if err != nil || !cfg.Enabled || cfg.Address != listen || cfg.Credential != "dot-cli:operator" {
		t.Fatalf("0600 configuration: %+v err=%v", cfg, err)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil || bytes.Contains(saved, []byte(token)) {
		t.Fatalf("reference-only configuration missing or contains token: %v", err)
	}

	// Rejection after import must leave both the store and saved bytes intact.
	out.Reset()
	err = dotTo(context.Background(), &out, []string{"pair", rightPath})
	if err == nil || out.Len() != 0 || store.writes != 1 || store.token != token || calls.Load() != 1 {
		t.Fatalf("0644 import after pairing: err=%v output=%q writes=%d status=%d", err, out.String(), store.writes, calls.Load())
	}
	if strings.Contains(err.Error(), token) {
		t.Fatal("rejection exposed pairing token")
	}
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(saved, after) {
		t.Fatalf("0644 import changed configuration: %v", err)
	}
	for _, path := range []string{leftPath, rightPath} {
		retained, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(retained, body) {
			t.Fatalf("pairing fixture changed or removed: %s: %v", path, err)
		}
	}
}
