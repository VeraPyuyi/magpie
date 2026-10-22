package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/settings"
)

type dotSwitchStore struct {
	dotFakeStore
	switchConfig func()
}

func (s *dotSwitchStore) Get(_, _ string) (string, error) {
	if s.switchConfig != nil {
		f := s.switchConfig
		s.switchConfig = nil
		f()
	}
	return s.token, nil
}

func TestDotCLISwitchBetweenLoadAndConnectionDoesNotJournal(t *testing.T) {
	dotSandbox(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer srv.Close()
	cfg := dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(srv.URL, "http://"), Credential: "A"}
	if err := dotbridge.SavePairing(cfg, "fixture-secret"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = dotbridge.LoadConfig()
	b := cfg
	b.Credential = "B"
	b.PairingGeneration = "replacement-B"
	raw, _ := json.Marshal(b)
	store := &dotSwitchStore{dotFakeStore: dotFakeStore{token: "fixture-secret"}, switchConfig: func() {
		// Simulates an independent atomic writer between config capture and use.
		if err := edit.WriteAtomic(filepath.Join(settings.Dir(), "dot-bridge.json"), raw); err != nil {
			t.Fatal(err)
		}
	}}
	dotbridge.SetCredentialStore(store)
	path := dotTempFile(t, "task.json")
	dotWriteFile(t, path, []byte(`{"request_id":"switch-id","mode":"task","task":"work","messages":[]}`))
	dotTestPrivate(t, path)
	var out bytes.Buffer
	if err := dotTo(t.Context(), &out, []string{"submit", "--file", path}); err == nil {
		t.Fatal("switched pairing accepted")
	}
	rows, err := dotbridge.RequestArchive()
	if err != nil || len(rows) != 0 || calls.Load() != 0 {
		t.Fatalf("switched operation leaked: rows=%v calls=%d err=%v", rows, calls.Load(), err)
	}
}

func TestDotCLIArchiveNamesOldPairingWithoutRemoteQuery(t *testing.T) {
	dotSandbox(t)
	cfg := dotbridge.Config{Enabled: true, Address: "127.0.0.1:9", Credential: "A"}
	if err := dotbridge.SavePairing(cfg, "fixture-secret"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = dotbridge.LoadConfig()
	scope := dotScope(cfg)
	sub := dotbridge.Submission{RequestID: "old-pending", Mode: dotbridge.ModeTask, Task: "work"}
	if _, _, err := dotbridge.Reserve(sub, scope); err != nil {
		t.Fatal(err)
	}
	if err := dotbridge.SavePairing(cfg, "new-secret"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := dotTo(t.Context(), &out, []string{"archives"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), scope) || !strings.Contains(out.String(), "old-pending") {
		t.Fatal(out.String())
	}
	current, _ := dotbridge.LoadConfig()
	items, _ := dotbridge.Requests(dotScope(current))
	if len(items) != 0 {
		t.Fatal("archive reassigned to current queue")
	}
}
