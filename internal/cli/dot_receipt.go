package cli

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/dotbridge"
)

// The client exports Resume's identity validation but Get/Cancel have no
// validator. Apply wire shape/terminal checks here, then use Resume to bind
// known requests to the journal's immutable digest/mode/session identity.
func dotValidateJob(j dotbridge.Job, id string) error {
	bad := fmt.Errorf("dot: invalid job receipt")
	digest, e := hex.DecodeString(j.RequestDigest)
	if j.Version != 1 || j.JobID != id || !dotID.MatchString(id) || !dotID.MatchString(j.RequestID) || e != nil || len(digest) != 32 || (j.Mode != dotbridge.ModeTask && j.Mode != dotbridge.ModeAnswer) || j.SessionID != "" && !dotID.MatchString(j.SessionID) {
		return bad
	}
	terminal := false
	switch j.State {
	case dotbridge.StateCompleted, dotbridge.StateFailed, dotbridge.StateCancelled:
		terminal = true
	case dotbridge.StateQueued, dotbridge.StateClaimed, dotbridge.StateRunning, dotbridge.StateWaitingUser, dotbridge.StateCancelRequested, dotbridge.StateOutcomeUnknown:
	default:
		return bad
	}
	if j.State == dotbridge.StateCancelRequested && !j.CancelRequested {
		return bad
	}
	if !terminal && j.Result != nil {
		return bad
	}
	if j.State == dotbridge.StateCompleted && (j.Result == nil || j.ErrorCode != "" || j.Result.ErrorCode != "" || j.Result.Cancelled) {
		return bad
	}
	if j.Result != nil {
		r := j.Result
		if !utf8.ValidString(r.Text) || len(r.Text) > 256*1024 || len(r.Artifacts) > 16 || r.Cancelled != (j.State == dotbridge.StateCancelled) {
			return bad
		}
		for _, a := range r.Artifacts {
			u, e := url.Parse(a.URL)
			if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				return bad
			}
		}
	}
	return nil
}
func dotKnownJob(ctx context.Context, c *dotbridge.Connection, scope, id string) (dotbridge.Job, error) {
	items, err := dotbridge.Requests(scope)
	if err != nil {
		return dotbridge.Job{}, err
	}
	for _, s := range items {
		if s.JobID == id {
			j, _, err := c.Resume(ctx, scope, s.RequestID)
			if err != nil {
				return j, err
			}
			return j, dotValidateJob(j, id)
		}
	}
	j, err := c.Get(ctx, id)
	if err != nil {
		return j, err
	}
	if err := dotValidateJob(j, id); err != nil {
		return j, err
	}
	// A request with uncertain acceptance may not yet have a journaled job ID.
	for _, s := range items {
		if s.RequestID == j.RequestID {
			if j.RequestDigest != s.Digest || j.Mode != s.Mode || j.SessionID != s.SessionID {
				return j, fmt.Errorf("dot: journal identity mismatch")
			}
			verified, _, err := c.Resume(ctx, scope, s.RequestID)
			if err != nil {
				return j, err
			}
			return verified, dotValidateJob(verified, id)
		}
	}
	return j, nil
}
func dotSameJob(a, b dotbridge.Job) bool {
	return a.JobID == b.JobID && a.RequestID == b.RequestID && a.RequestDigest == b.RequestDigest && a.Mode == b.Mode && a.SessionID == b.SessionID
}
