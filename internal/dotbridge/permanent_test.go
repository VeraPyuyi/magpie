package dotbridge

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPermanentFailsClosed(t *testing.T) {
	now := time.Now()
	for _, mutate := range []func(*Status){
		func(s *Status) { s.Version = 0 },
		func(s *Status) { s.Enabled = false },
		func(s *Status) { s.Paired = false },
		func(s *Status) { s.Ready = false },
		func(s *Status) { s.VerifiedAt = time.Time{} },
		func(s *Status) { s.Subscription.State = "" },
		func(s *Status) { s.Subscription.State = "expired" },
	} {
		st := Status{Version: 1, Enabled: true, Paired: true, Ready: true, VerifiedAt: now.Add(-time.Minute), Subscription: SubscriptionStatus{State: "active"}}
		mutate(&st)
		if st.Authorized(now) {
			t.Fatalf("invalid permanent status admitted: %+v", st)
		}
	}
}

func TestPermanentMixedBounds(t *testing.T) {
	now := time.Now()
	for _, subscription := range []bool{false, true} {
		st := Status{Version: 1, Enabled: true, Paired: true, Ready: true, VerifiedAt: now.Add(-time.Hour), Subscription: SubscriptionStatus{State: "active"}}
		past := now.Add(-time.Second)
		if subscription {
			st.Subscription.ExpiresAt = &past
		} else {
			st.ExpiresAt = &past
		}
		if st.Authorized(now) || st.DisplayCode(now) != "subscription_expired" || st.SubscriptionTerm() != "" {
			t.Fatalf("finite bound ignored: %+v", st)
		}
	}
}

func TestPermanentStatus(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, dates, state, proof string
		ready, want               bool
	}{
		{"absent", "", "active", `"verified_at":"2026-10-23T11:00:00Z",`, true, true},
		{"null", `,"expires_at":null,"refresh_before":null`, "active", `"verified_at":"2026-10-23T11:00:00Z",`, true, true},
		{"zero", `,"expires_at":"0001-01-01T00:00:00Z"`, "active", `"verified_at":"2026-10-23T11:00:00Z",`, true, true},
		{"not ready", "", "active", `"verified_at":"2026-10-23T11:00:00Z",`, false, false},
		{"missing proof", "", "active", "", true, false},
		{"zero proof", "", "active", `"verified_at":"0001-01-01T00:00:00Z",`, true, false},
		{"zero inactive", `,"expires_at":"0001-01-01T00:00:00Z"`, "inactive", `"verified_at":"2026-10-23T11:00:00Z",`, true, false},
		{"future proof", "", "active", `"verified_at":"2026-10-24T11:00:00Z",`, true, false},
		{"inactive", "", "inactive", `"verified_at":"2026-10-23T11:00:00Z",`, true, false},
		{"expired", `,"expires_at":"2026-10-22T11:00:00Z"`, "active", `"verified_at":"2026-10-23T11:00:00Z",`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ready := "false"
			if tc.ready {
				ready = "true"
			}
			raw := `{"version":1,"enabled":true,"paired":true,"ready":` + ready + `,` + tc.proof + `"subscription":{"state":"` + tc.state + `"` + tc.dates + `}` + tc.dates + `}`
			var st Status
			if err := json.Unmarshal([]byte(raw), &st); err != nil {
				t.Fatal(err)
			}
			if (tc.name == "null" || tc.name == "absent") && (st.ExpiresAt != nil || st.Subscription.ExpiresAt != nil || st.Subscription.RefreshBefore != nil) {
				t.Fatal("absent/null dates must decode to nil")
			}
			if got := st.Authorized(now); got != tc.want {
				t.Fatalf("Authorized = %v, want %v", got, tc.want)
			}
			if tc.want && st.DisplayCode(now) != "ready" {
				t.Fatalf("display = %s", st.DisplayCode(now))
			}
			if tc.name == "expired" && st.DisplayCode(now) != "subscription_expired" {
				t.Fatal(st.DisplayCode(now))
			}
		})
	}
}
