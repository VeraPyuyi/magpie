package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

var errDotNotPaired = errors.New("dot_bridge_not_paired")

func enableDotBridgeForTest(t *testing.T) *httptest.Server {
	t.Helper()
	store := newDotMemStore()
	dotbridge.SetCredentialStore(store)
	t.Cleanup(func() { dotbridge.SetCredentialStore(nil) })
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	now := time.Now().UTC()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dot-bridge/v1/status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": 1, "enabled": true, "paired": true, "ready": true, "code": "ready",
			"verified_at": now.Add(-time.Second), "expires_at": now.Add(time.Minute),
			"subscription": map[string]any{"state": "active", "expires_at": now.Add(5 * time.Minute)},
			"capabilities": map[string]bool{"text": true, "tasks": false},
		})
	}))
	t.Cleanup(srv.Close)
	cfg := dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(srv.URL, "http://"), Credential: "pair-ref", AnswerWaitSeconds: 120}
	if err := dotbridge.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := dotbridge.SavePairing(cfg, "paired-token"); err != nil {
		t.Fatal(err)
	}
	conn, err := dotbridge.PairedConnection()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	return srv
}

type dotMemStore struct{ data map[string]string }

func newDotMemStore() *dotMemStore { return &dotMemStore{data: map[string]string{}} }

func (s *dotMemStore) key(service, account string) string { return service + "\x00" + account }

func (s *dotMemStore) Get(service, account string) (string, error) {
	v, ok := s.data[s.key(service, account)]
	if !ok {
		return "", errDotNotPaired
	}
	return v, nil
}

func (s *dotMemStore) Set(service, account, secret string) error {
	s.data[s.key(service, account)] = secret
	return nil
}

func (s *dotMemStore) Delete(service, account string) error {
	delete(s.data, s.key(service, account))
	return nil
}

func dotTestConnection(t *testing.T, srv *httptest.Server, token string) *dotbridge.Connection {
	host := strings.TrimPrefix(srv.URL, "http://")
	conn, err := dotbridge.NewConnection(host, func() (string, error) { return token, nil })
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestDotConfiguredAutoGroupRetainsUnavailableMember(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Provider{ID: "other", Name: "Other", Key: "k", Models: []string{"text"}, Chat: "http://127.0.0.1:1/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveGroup(Group{ID: "auto-text", Name: "Text", Members: []string{"dot/text", "other/text"}}); err != nil {
		t.Fatal(err)
	}
	g, ms, ok := FindGroup("group/auto-text")
	if !ok || len(g.Members) != 2 || g.Members[0] != "dot/text" || len(ms) != 1 || ms[0].Provider.IsDot() {
		t.Fatalf("configuration/execution mismatch: %+v %+v %v", g, ms, ok)
	}
}

func TestDotProviderIsOptIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, ok := DotProvider(); ok {
		t.Fatal("disabled bridge must not expose dot provider")
	}
	enableDotBridgeForTest(t)
	p, ok := DotProvider()
	if !ok || !p.IsDot() || p.Key != "" || p.Account != nil {
		t.Fatalf("unexpected dot provider %+v ok=%v", p, ok)
	}
	if err := Save(Provider{ID: DotID, Name: "User Dot", Chat: "http://127.0.0.1:1/v1"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range All() {
		if p.ID == DotID && p.IsDot() {
			t.Fatal("stored dot provider collision must refuse built-in dot")
		}
	}
}

// Bare text follows the normal provider order, so enabling Dot never
// shadows an existing provider exposing its own text model.
func TestDotBareTextResolution(t *testing.T) {
	enableDotBridgeForTest(t)
	p, model, ok := Resolve("text")
	if !ok || !p.IsDot() || model != DotTextModel {
		t.Fatalf("Dot-only bare text: provider=%s model=%s ok=%v", p.ID, model, ok)
	}
	if err := Save(Provider{ID: "other", Name: "Other", Key: "k", Models: []string{"text"}, Chat: "http://127.0.0.1:1/v1"}); err != nil {
		t.Fatal(err)
	}
	p, model, ok = Resolve("text")
	if !ok || p.IsDot() || p.ID != "other" || model != "text" {
		t.Fatalf("shared bare text: provider=%s model=%s ok=%v", p.ID, model, ok)
	}
	p, _, ok = Resolve("dot/text")
	if !ok || !p.IsDot() {
		t.Fatal("qualified dot/text must still resolve to Dot")
	}
}

func TestDotModelAllowList(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if len(All()) != 0 {
		for _, p := range All() {
			if p.IsDot() {
				t.Fatal("unready dot must not appear in All")
			}
		}
	}
	enableDotBridgeForTest(t)
	var dot *Provider
	for _, p := range All() {
		if p.IsDot() {
			dot = &p
			break
		}
	}
	if dot == nil {
		t.Fatal("ready dot missing from All")
	}
	if got := dot.Exposed(); len(got) != 1 || got[0].ID != DotTextModel {
		t.Fatalf("exposed=%v", got)
	}
	if _, _, ok := Resolve(DotID + "/" + DotTextModel); !ok {
		t.Fatal("dot/text must resolve")
	}
	for _, id := range []string{"dot/agent", DotID + "/agent", "dot/unknown"} {
		if _, _, ok := Resolve(id); ok {
			t.Fatalf("%s must not resolve", id)
		}
	}
	if rs := dot.Test(context.Background()); rs[0].Error != "" {
		t.Fatalf("Test must not send generic probe: %+v", rs)
	}
	if rs := dot.TestModels(context.Background(), []string{DotTextModel, "agent"}); !rs[0].OK || rs[1].Error == "" {
		t.Fatalf("TestModels: %+v", rs)
	}
}
