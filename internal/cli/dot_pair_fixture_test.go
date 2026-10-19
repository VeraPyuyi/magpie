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

// prepare sets the fixture's platform access to the case under test. It is
// called after the file is written, so a platform can apply a real chmod or a
// real DACL and assert it took effect.
type prepare func(*testing.T, string)

// dotPairFixtureBody is a valid version 1 pairing file body for the fixture
// cases that build their own file instead of calling dotPairFixture.
func dotPairingFixtureBody(t *testing.T) []byte {
	t.Helper()
	file := dotPairingFile{
		Version: 1, ClientID: "operator", QueueID: "operator",
		Token: "sentinel-private-token", Listen: "127.0.0.1:1",
		CreatedAt: time.Now(),
	}
	b, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// dotPairFixture builds an otherwise-valid version 1 pairing file whose status
// endpoint the caller controls, and hands the file to prepare. The address
// fields are rewritten by the case-specific transform before the file is
// written, so the fixture stays a real DotControl PairingFile shape.
func dotPairFixture(t *testing.T, p prepare, transform func(*dotPairingFile, string), status func(http.ResponseWriter)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status(w)
	}))
	t.Cleanup(srv.Close)
	file := dotPairingFile{
		Version: 1, ClientID: "operator", QueueID: "operator",
		Token: "sentinel-private-token", Listen: strings.TrimPrefix(srv.URL, "http://"),
		CreatedAt: time.Now(),
	}
	if transform != nil {
		transform(&file, srv.URL)
	}
	b, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	path := dotTempFile(t, "pair.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if p != nil {
		p(t, path)
	}
	return path
}

// dotPairFixtureBytes writes the given file body instead of a marshalled
// fixture, for the malformed-JSON cases.
func dotPairFixtureBytes(t *testing.T, body []byte, p prepare, status func(http.ResponseWriter)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status(w)
	}))
	t.Cleanup(srv.Close)
	path := dotTempFile(t, "pair.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if p != nil {
		p(t, path)
	}
	return path
}

// dotPairAccepted proves an authenticated import writes exactly one token to
// the store, saves a reference-only config, and keeps the pairing file.
func dotPairAccepted(t *testing.T, p prepare) {
	t.Helper()
	store := dotSandbox(t)
	path := dotPairFixture(t, p, nil, dotStatusPaired)
	var out bytes.Buffer
	if err := dotTo(context.Background(), &out, []string{"pair", path}); err != nil {
		t.Fatal(err)
	}
	if store.writes != 1 || store.token != "sentinel-private-token" {
		t.Fatal("import did not store the pairing token", store.writes, store.token)
	}
	cfg, err := dotbridge.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.Credential != "dot-cli:operator" {
		t.Fatal("config is not enabled with a credential reference", cfg)
	}
	saved, err := os.ReadFile(filepath.Join(settings.Dir(), "dot-bridge.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "sentinel-private-token") {
		t.Fatal("token was written into the config file")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("pairing file was removed", err)
	}
	if strings.Contains(out.String(), "sentinel-private-token") {
		t.Fatal("token was printed")
	}
}

// dotPairRejected proves a rejected import leaves no vault entry, no config
// file, no output and no accepted credential, whether or not the status
// endpoint was reached. wantCalls is the expected number of status requests.
func dotPairRejected(t *testing.T, why string, wantCalls int32, transform func(*dotPairingFile, string), body []byte, p prepare) {
	t.Helper()
	store := dotSandbox(t)
	var calls atomic.Int32
	status := func(w http.ResponseWriter) {
		calls.Add(1)
		dotStatusPaired(w)
	}
	var path string
	if body != nil {
		path = dotPairFixtureBytes(t, body, p, status)
	} else {
		path = dotPairFixture(t, p, transform, status)
	}
	var out bytes.Buffer
	err := dotTo(context.Background(), &out, []string{"pair", path})
	if err == nil {
		t.Fatalf("%s: import was accepted", why)
	}
	if store.writes != 0 {
		t.Fatalf("%s: %d vault writes", why, store.writes)
	}
	if out.Len() != 0 {
		t.Fatalf("%s: printed output: %s", why, out.String())
	}
	if _, statErr := os.Stat(filepath.Join(settings.Dir(), "dot-bridge.json")); !os.IsNotExist(statErr) {
		t.Fatalf("%s: config was written: %v", why, statErr)
	}
	if strings.Contains(out.String(), "sentinel-private-token") || strings.Contains(err.Error(), "sentinel-private-token") {
		t.Fatalf("%s: token leaked", why)
	}
	if got := calls.Load(); got != wantCalls {
		t.Fatalf("%s: status calls=%d, want %d", why, got, wantCalls)
	}
}
