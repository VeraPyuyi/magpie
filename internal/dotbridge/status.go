package dotbridge

import "time"

// Authorized uses the authenticated server's current lease. Magpie never extends
// the grant or invents a proof lifetime from its local clock.
func (s Status) Authorized(now time.Time) bool {
	return s.Version == 1 && s.Enabled && s.Paired && s.Ready && s.Subscription.State == "active" && !s.VerifiedAt.IsZero() && !s.VerifiedAt.After(now) && now.Before(s.ExpiresAt) && now.Before(s.Subscription.ExpiresAt)
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
	if s.Subscription.State == "expired" || (!s.Subscription.ExpiresAt.IsZero() && !now.Before(s.Subscription.ExpiresAt)) {
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
