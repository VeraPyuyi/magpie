package dotbridge

import "time"

func expired(at *time.Time, now time.Time) bool {
	return at != nil && !at.IsZero() && !now.Before(*at)
}

// SubscriptionTerm labels an active subscription without a finite expiration.
// It does not grant authorization: readiness and proof are checked separately.
func (s Status) SubscriptionTerm() string {
	if s.Subscription.State == "active" && (s.ExpiresAt == nil || s.ExpiresAt.IsZero()) && (s.Subscription.ExpiresAt == nil || s.Subscription.ExpiresAt.IsZero()) {
		return "permanent"
	}
	return ""
}

// Authorized uses the authenticated server's current lease. Magpie never extends
// the grant or invents a proof lifetime from its local clock.
func (s Status) Authorized(now time.Time) bool {
	return s.Version == 1 && s.Enabled && s.Paired && s.Ready && s.Subscription.State == "active" && !s.VerifiedAt.IsZero() && !s.VerifiedAt.After(now) && !expired(s.ExpiresAt, now) && !expired(s.Subscription.ExpiresAt, now)
}

// DisplayCode preserves specific server reasons and derives expiration for v1
// servers that report every readiness failure as probe_required.
func (s Status) DisplayCode(now time.Time) string {
	if !s.Enabled {
		return "disabled"
	}
	if !s.Paired {
		return "unpaired"
	}
	if s.Code == "bridge_paused" || s.Code == "access_revoked" {
		return s.Code
	}
	if s.Subscription.State == "expired" || expired(s.ExpiresAt, now) || expired(s.Subscription.ExpiresAt, now) {
		return "subscription_expired"
	}
	if s.Authorized(now) {
		return "ready"
	}
	if s.Code != "" && s.Code != "ready" {
		return s.Code
	}
	return "probe_required"
}
