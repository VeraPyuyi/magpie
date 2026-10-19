package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
)

const dotRequestHeader = "X-Magpie-Dot-Request-ID"
const (
	dotErrPending   = "dot_job_pending"
	dotErrUnknown   = "dot_acceptance_unknown"
	dotErrFailed    = "dot_job_failed"
	dotErrCancelled = "dot_job_cancelled"
)

var (
	errDotNoAnswer         = fmt.Errorf("dot job has no answer text")
	errDotUnsupportedProto = fmt.Errorf("dot answer unsupported protocol")
	dotIDPattern           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type dotAttemptKey struct{}
type dotSubmissionKey struct{}
type dotPairingKey struct{}

func withDotAttempt(ctx context.Context, attempt *dotbridge.Attempt) context.Context {
	return context.WithValue(ctx, dotAttemptKey{}, attempt)
}
func dotAttemptOf(ctx context.Context) *dotbridge.Attempt {
	a, _ := ctx.Value(dotAttemptKey{}).(*dotbridge.Attempt)
	return a
}
func dotHashParts(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:", len(p))
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}
func dotScope(r *http.Request) string {
	key := access.Caller(r.Context()).KeyID
	if key == "" {
		key = "local"
	}
	identity, _ := r.Context().Value(dotPairingKey{}).(string)
	if identity == "" {
		cfg, err := dotbridge.LoadConfig()
		if err == nil { identity = dotbridge.PairingIdentity(cfg) }
	}
	return identity + ":" + dotHashParts(key, callerOf(r).agent)
}
func dotRequestID(r *http.Request) (string, error) {
	if id := r.Header.Get(dotRequestHeader); id != "" {
		if !dotIDPattern.MatchString(id) {
			return "", fmt.Errorf("invalid %s", dotRequestHeader)
		}
		return "req-" + dotHashParts(dotScope(r), id), nil
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "req-" + hex.EncodeToString(b[:]), nil
}
func dotSessionID(r *http.Request, requestID string) string {
	if session := sessionOf(r.Header); session != "" {
		return "sess-" + dotHashParts(dotScope(r), session)
	}
	return "sess-" + dotHashParts(dotScope(r), requestID)
}

// prepareDot binds immutable identity and the exact client-owned Attempt before
// attempt runs. Neither headers nor a vendor response can set its replay state.
func prepareDot(r *http.Request, from provider.Protocol, body []byte) (*http.Request, error) {
	cfg, err := dotbridge.LoadConfig()
	if err != nil { return r, err }
	identity := dotbridge.PairingIdentity(cfg)
	if !cfg.Enabled || identity == "" { return r, fmt.Errorf("dot pairing unavailable") }
	r = r.WithContext(context.WithValue(r.Context(), dotPairingKey{}, identity))
	id, err := dotRequestID(r)
	if err != nil {
		return r, err
	}
	sub, err := dotSubmission(from, body, id, dotSessionID(r, id))
	if err != nil {
		return r, err
	}
	a, err := dotbridge.NewAttempt(sub)
	if err != nil {
		return r, err
	}
	ctx := context.WithValue(withDotAttempt(r.Context(), a), dotSubmissionKey{}, sub)
	return r.WithContext(ctx), nil
}

func (s *Server) serveDot(w http.ResponseWriter, r *http.Request, from provider.Protocol, p provider.Provider, model string, body []byte, call *Call) (int, string) {
	a := dotAttemptOf(r.Context())
	sub, ok := r.Context().Value(dotSubmissionKey{}).(dotbridge.Submission)
	if a == nil || !ok {
		return writeError(w, from, 500, "dot attempt not bound"), "dot attempt not bound"
	}
	conn, err := dotbridge.PairedConnection()
	if err != nil {
		return writeError(w, from, 503, "dot bridge unavailable"), "dot bridge unavailable"
	}
	cfg, err := dotbridge.LoadConfig()
	identity, _ := r.Context().Value(dotPairingKey{}).(string)
	if err != nil || identity == "" || identity != conn.PairingIdentity() || identity != dotbridge.PairingIdentity(cfg) {
		return writeError(w, from, 503, "dot bridge configuration unavailable"), "dot bridge configuration unavailable"
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.AnswerWaitSeconds)*time.Second)
	defer cancel()
	call.To = from // Queue attempts belong in the ledger even without an answer.
	job, err := conn.Enqueue(ctx, sub, dotScope(r), a)
	if err != nil {
		if a.Snapshot().BlockReplay {
			return 409, dotErrUnknown
		}
		if dotbridge.IsDigestConflict(err) {
			return writeError(w, from, 409, "dot request identity conflicts with its saved content"), "dot request identity conflict"
		}
		return writeError(w, from, 503, "dot bridge enqueue failed"), "dot bridge enqueue failed"
	}
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	for {
		if code := dotJobError(job); code != "" {
			return 409, code
		}
		if job.State == dotbridge.StateCompleted {
			b, err := dotAnswer(from, job)
			if err != nil {
				return 409, dotErrFailed
			}
			var rendered struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(b, &rendered)
			call.Usage.ResponseID = rendered.ID
			// The hold writer serializes header initialization with keepQuiet.
			// Stream events use wholeAsStream's begun writer and never mutate Header.
			if streamOf(body) {
				w.WriteHeader(200)
				wholeAsStream(w, from, b)
			} else {
				w.WriteHeader(200)
				_, _ = w.Write(b)
			}
			return 200, ""
		}
		select {
		case <-ctx.Done():
			return 409, dotErrPending
		case <-poll.C:
			// ByRequest validates identity and the full receipt on every read. Get
			// currently returns an unchecked DTO, so it cannot authenticate an answer.
			job, err = conn.ByRequest(ctx, a)
			if err != nil {
				return 409, dotErrUnknown
			}
		}
	}
}
func dotJobError(job dotbridge.Job) string {
	if job.ErrorCode != "" && job.State != dotbridge.StateCancelled {
		return dotErrFailed
	}
	switch job.State {
	case dotbridge.StateQueued, dotbridge.StateClaimed, dotbridge.StateRunning, dotbridge.StateWaitingUser:
		return ""
	case dotbridge.StateFailed, dotbridge.StateOutcomeUnknown:
		return dotErrFailed
	case dotbridge.StateCancelled:
		return dotErrCancelled
	case dotbridge.StateCancelRequested:
		return dotErrPending
	case dotbridge.StateCompleted:
		if job.CancelRequested || job.Result == nil || job.Result.Cancelled || job.Result.ErrorCode != "" || job.ErrorCode != "" || !utf8.ValidString(job.Result.Text) || strings.TrimSpace(job.Result.Text) == "" {
			return dotErrFailed
		}
		return ""
	default:
		return dotErrUnknown
	}
}
func dotFailureMessage(snap dotbridge.Snapshot, code string) string {
	return fmt.Sprintf("%s: inspect the existing Dot request %s (job %s); automatic_redispatch:false", code, snap.RequestID, snap.JobID)
}

// dotFailure is called only after the hold writer's watch has joined. It
// writes structured errors directly, avoiding encoders that emit a fake start.
func dotFailure(w http.ResponseWriter, kept *keptAlive, proto provider.Protocol, snap dotbridge.Snapshot, code string) int {
	fields := map[string]any{"code": code, "type": "api_error", "message": dotFailureMessage(snap, code), "request_id": snap.RequestID, "automatic_redispatch": false, "stable_request_id_required_for_client_retries": true}
	if snap.JobID != "" {
		fields["job_id"] = snap.JobID
	}
	if kept != nil && kept.sent {
		sw := newSSEWriter(w)
		sw.begun = true
		if proto == provider.Responses {
			fields["type"] = "error"
			sw.event("error", fields)
		} else {
			sw.event("", map[string]any{"error": fields})
		}
	} else {
		b, _ := json.Marshal(map[string]any{"error": fields})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		_, _ = w.Write(b)
	}
	return 409
}
