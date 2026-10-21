package gui

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

var errDotGatewayAmbiguous = errors.New("dot_gateway_request_ambiguous")

func dotGatewayScopeAllowed(scope, identity string) bool {
	if identity == "" || strings.HasPrefix(scope, "gui:") || strings.HasPrefix(scope, "cli:") {
		return false
	}
	tail := strings.TrimPrefix(scope, identity+":")
	if tail == scope || len(tail) != 64 {
		return false
	}
	for _, c := range tail {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// dotGatewaySnapshots returns bounded identity snapshots for the current
// pairing's gateway journal scopes. Scope strings and digests are omitted.
type dotGatewayDisplay struct {
	RequestID string `json:"request_id"`
	JobID     string `json:"job_id,omitempty"`
	State     string `json:"state"`
	Mode      string `json:"mode"`
}

func dotGatewaySnapshots(cfg dotbridge.Config) ([]dotGatewayDisplay, error) {
	identity := dotbridge.PairingIdentity(cfg)
	if identity == "" {
		return nil, nil
	}
	archive, err := dotbridge.RequestArchive()
	if err != nil {
		return nil, err
	}
	var out []dotGatewayDisplay
	for _, item := range archive {
		if !dotGatewayScopeAllowed(item.Scope, identity) {
			continue
		}
		out = append(out, dotGatewayDisplay{RequestID: item.RequestID, JobID: item.JobID, State: item.State, Mode: item.Mode})
	}
	if len(out) > 100 {
		out = out[len(out)-100:]
	}
	return out, nil
}

func dotGatewayScope(cfg dotbridge.Config, requestID string) (string, error) {
	identity := dotbridge.PairingIdentity(cfg)
	archive, err := dotbridge.RequestArchive()
	if err != nil {
		return "", err
	}
	var scope string
	for _, item := range archive {
		if item.RequestID != requestID || !dotGatewayScopeAllowed(item.Scope, identity) {
			continue
		}
		if scope != "" && scope != item.Scope {
			return "", errDotGatewayAmbiguous
		}
		scope = item.Scope
	}
	if scope == "" {
		return "", errors.New("dot_bridge_request_not_found")
	}
	return scope, nil
}

func dotGatewayRequest(w http.ResponseWriter, r *http.Request, cfg dotbridge.Config, c *dotbridge.Connection, requestID string) {
	scope, err := dotGatewayScope(cfg, requestID)
	if err != nil {
		if errors.Is(err, errDotGatewayAmbiguous) {
			dotReplyError(w, 409, "outcome_unknown")
			return
		}
		dotReplyError(w, 404, "not_found")
		return
	}
	if c == nil {
		original, recovery, recoveryErr := dotbridge.RecoveryPairingSnapshot()
		if recoveryErr != nil || dotbridge.PairingIdentity(original) != dotbridge.PairingIdentity(cfg) {
			dotReplyError(w, 503, "unverified")
			return
		}
		c = recovery
	}
	job, _, err := c.Resume(r.Context(), scope, requestID)
	if err != nil {
		dotReplyError(w, 409, "outcome_unknown")
		return
	}
	writeJSON(w, struct {
		RequestID string            `json:"request_id"`
		JobID     string            `json:"job_id"`
		Mode      string            `json:"mode"`
		State     string            `json:"state"`
		Summary   string            `json:"summary,omitempty"`
		UpdatedAt time.Time         `json:"updated_at"`
		Result    *dotbridge.Result `json:"result,omitempty"`
	}{job.RequestID, job.JobID, job.Mode, job.State, job.Summary, job.UpdatedAt, job.Result})
}
