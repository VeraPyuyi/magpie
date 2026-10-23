package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

func TestDotCLIPermanentStatus(t *testing.T) {
	dotSandbox(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":1,"enabled":true,"paired":true,"ready":true,"verified_at":"2026-01-01T00:00:00Z","expires_at":"0001-01-01T00:00:00Z","subscription":{"state":"active","expires_at":"0001-01-01T00:00:00Z","refresh_before":null},"capabilities":{"tasks":true}}`))
	}))
	defer srv.Close()
	if err := dotbridge.SaveConfig(dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(srv.URL, "http://"), Credential: "cli"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := dotTo(context.Background(), &out, []string{"status"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"subscription_term":"permanent"`) || strings.Contains(out.String(), "0001-01-01") {
		t.Fatalf("status: %s", &out)
	}
	st := dotbridge.Status{Version: 1, Enabled: true, Paired: true, Ready: true, VerifiedAt: time.Now().Add(-time.Minute), Subscription: dotbridge.SubscriptionStatus{State: "active"}, Capabilities: dotbridge.Capabilities{Tasks: true}}
	if !dotTaskReady(st, time.Now()) {
		t.Fatal("permanent tasks unavailable")
	}
	st.Ready = false
	if dotTaskReady(st, time.Now()) {
		t.Fatal("unverified tasks available")
	}
}
