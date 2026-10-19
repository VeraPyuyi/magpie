package gateway

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

func enableDotBridgeForGateway(t *testing.T) *httptest.Server {
	t.Helper()
	dotbridge.SetCredentialStore(newDotMemStore())
	t.Cleanup(func() { dotbridge.SetCredentialStore(nil) })
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
	host := strings.TrimPrefix(srv.URL, "http://")
	conn, err := dotbridge.NewConnection(host, func() (string, error) { return "paired-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	return srv
}
