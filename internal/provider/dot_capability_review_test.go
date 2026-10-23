package provider

import (
	"encoding/json"
	"github.com/yetone/magpie/internal/dotbridge"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDotEachModelRequiresItsCapability(t *testing.T) {
	for _, tasks := range []bool{false, true} {
		t.Run(map[bool]string{false: "text-only", true: "task-only"}[tasks], func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			dotbridge.SetCredentialStore(newDotMemStore())
			t.Cleanup(func() { dotbridge.SetCredentialStore(nil) })
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				now := time.Now()
				json.NewEncoder(w).Encode(dotbridge.Status{Version: 1, Enabled: true, Paired: true, Ready: true, VerifiedAt: now.Add(-time.Second), Subscription: dotbridge.SubscriptionStatus{State: "active"}, Capabilities: dotbridge.Capabilities{Text: !tasks, Tasks: tasks}})
			}))
			defer srv.Close()
			if err := dotbridge.SavePairing(dotbridge.Config{Address: strings.TrimPrefix(srv.URL, "http://"), Credential: "fixture"}, "token"); err != nil {
				t.Fatal(err)
			}
			c, err := dotbridge.PairedConnection()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = c.Status(t.Context()); err != nil {
				t.Fatal(err)
			}
			want, blocked := "text", "task"
			if tasks {
				want, blocked = blocked, want
			}
			p, ok := DotProvider()
			if !ok {
				t.Fatal("provider missing")
			}
			for _, list := range [][]string{modelIDs(p.Available()), modelIDs(p.Exposed())} {
				if len(list) != 1 || list[0] != want {
					t.Errorf("models=%v want %s", list, want)
				}
			}
			if _, _, ok := Resolve("dot/" + blocked); ok {
				t.Errorf("resolved unauthorized %s", blocked)
			}
			if _, _, ok := Resolve("dot/" + want); !ok {
				t.Errorf("eligible %s missing", want)
			}
			for _, e := range Served() {
				if e.Provider.IsDot() && e.Model != want {
					t.Errorf("served unauthorized %s", e.Model)
				}
			}
		})
	}
}
