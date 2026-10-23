package dotbridge

import (
	"testing"
	"time"
)

func TestDotAuthoritativeLeaseAndDisplayReason(t *testing.T) {
	now := time.Now()
	st := Status{Version: 1, Enabled: true, Paired: true, Ready: true, Code: "ready", VerifiedAt: now.Add(-time.Hour), ExpiresAt: new(now.Add(time.Hour)), Subscription: SubscriptionStatus{State: "active", ExpiresAt: new(now.Add(time.Hour))}}
	if !st.Authorized(now) {
		t.Fatal("current authenticated lease must permit old verified proof")
	}
	st.Ready = false
	st.Code = "probe_required"
	st.Subscription.ExpiresAt = new(now.Add(-time.Second))
	if st.Authorized(now) || st.DisplayCode(now) != "subscription_expired" {
		t.Fatalf("lost actual expiry: %+v", st)
	}
	st.Subscription.ExpiresAt = new(now.Add(time.Hour))
	if st.DisplayCode(now) != "probe_required" {
		t.Fatal("unverified active subscription is not expired")
	}
	st.Code = "bridge_paused"
	if st.DisplayCode(now) != "bridge_paused" {
		t.Fatal("lost observed pause")
	}
}

func TestDotAcceptedJobIdentityCannotChange(t *testing.T) {
	sub := Submission{RequestID: "req-stable", SessionID: "sess-stable", Mode: ModeAnswer, Messages: []Message{{Role: "user", Content: "hi"}}}
	a, err := NewAttempt(sub)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{Version: 1, JobID: "job-first", RequestID: sub.RequestID, SessionID: sub.SessionID, Mode: sub.Mode, RequestDigest: a.Snapshot().Digest, State: StateQueued}
	if err := a.accept(job); err != nil {
		t.Fatal(err)
	}
	job.JobID = "job-replaced"
	if err := a.accept(job); err == nil {
		t.Fatal("accepted original job ID was replaced")
	}
	if a.Snapshot().JobID != "job-first" {
		t.Fatal("original identity changed on rejection")
	}
}
