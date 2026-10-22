package gui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

func TestDotGUIStatusNeverPaired(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	r := httptest.NewRequest("GET", "http://127.0.0.1:3999/api/dot/status", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	Handler(nil, nil).ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"code":"unpaired"`) {
		t.Fatalf("never paired: %d %s", w.Code, w.Body)
	}
}
func TestDotGUIExpiredSubscriptionReason(t *testing.T) {
	st := dotbridge.Status{Version: 1, Enabled: true, Paired: true, Code: "probe_required", Subscription: dotbridge.SubscriptionStatus{State: "active", ExpiresAt: time.Now().Add(-time.Hour)}}
	if st.DisplayCode(time.Now()) != "subscription_expired" {
		t.Fatal("actual subscription expiry erased into probe_required")
	}
}

func TestDotGUIContinuousLease(t *testing.T) {
	now := time.Now()
	st := dotbridge.Status{Version: 1, Enabled: true, Paired: true, Ready: true, VerifiedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Subscription: dotbridge.SubscriptionStatus{State: "active", ExpiresAt: now.Add(time.Hour)}, Capabilities: dotbridge.Capabilities{Tasks: true}}
	if !dotTaskAvailable(st) {
		t.Fatal("authenticated future lease rejected because proof is older than ten minutes")
	}
	st.Subscription.ExpiresAt = now.Add(-time.Second)
	if dotTaskAvailable(st) {
		t.Fatal("expired subscription admitted")
	}
}
