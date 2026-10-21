package gateway

import (
	"context"
	"net/http"
	"strings"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
)

type dotOriginalKey struct{}

// bindDotOriginal runs at the decoded body-read boundary before request rewrites,
// hooks or middleware. Exact bytes are hashed; no original content is persisted.
func bindDotOriginal(r *http.Request, from provider.Protocol, body []byte) {
	if r.Header.Get(dotRequestHeader) == "" {
		return
	}
	if r.Context().Value(dotOriginalKey{}) != nil {
		return
	}
	key := access.Caller(r.Context()).KeyID
	if key == "" {
		key = "local"
	}
	fingerprint := dotHashParts("dot-original-v1", key, callerOf(r).agent, string(from), r.URL.Path, modelOf(body), sessionOf(r.Header), string(body))
	*r = *r.WithContext(context.WithValue(r.Context(), dotOriginalKey{}, fingerprint))
}

func dotOriginalOf(r *http.Request) string {
	v, _ := r.Context().Value(dotOriginalKey{}).(string)
	return v
}

type dotRecoveryWriter struct {
	http.ResponseWriter
	status int
}

func (w *dotRecoveryWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// recoverDotOwner runs before model resolution: a hidden model, reordered group
// or replacement pairing cannot turn an accepted stable identity into new work.
// A body changed by middleware may not be reproducible here. Such a conflict is
// explicit and requires recovery by original request ID, never dispatch.
func (s *Server) recoverDotOwner(w http.ResponseWriter, r *http.Request, from provider.Protocol, body []byte) bool {
	header := r.Header.Get(dotRequestHeader)
	if header == "" {
		return false
	}
	if !dotIDPattern.MatchString(header) {
		writeError(w, from, 400, "invalid Dot request identity; automatic_redispatch:false")
		return true
	}
	caller := access.Caller(r.Context())
	key := caller.KeyID
	if key == "" {
		key = "local"
	}
	callerScope := dotHashParts(key, callerOf(r).agent)
	records, err := dotbridge.RequestArchive()
	if err != nil {
		writeError(w, from, 409, "dot journal unavailable; automatic_redispatch:false")
		return true
	}
	var owner *dotbridge.ArchivedRequest
	for i := range records {
		rec := &records[i]
		if rec.Scope != callerScope && !strings.HasSuffix(rec.Scope, ":"+callerScope) {
			continue
		}
		if rec.RequestID == "req-"+dotHashParts(rec.Scope, header) && rec.BlockReplay {
			owner = rec
			break
		}
	}
	if owner == nil {
		return false
	}
	fail := func(code string) bool { dotFailure(w, nil, from, owner.Snapshot, code); return true }
	// Gateway authentication has already run. Recheck current model restrictions
	// before returning a result; a key's identity alone is not current authority.
	if who, held := keyHolds(r); held && !who.Allows("dot/text") {
		writeError(w, from, 403, "Dot recovery is outside the gateway key's models")
		return true
	}
	if _, held := accountHolds(r); held {
		writeError(w, from, 403, "Dot recovery is outside the gateway key's accounts")
		return true
	}
	if owner.OriginalFingerprint == "" || dotOriginalOf(r) == "" {
		return fail("dot_original_fingerprint_required")
	}
	if owner.OriginalFingerprint != dotOriginalOf(r) {
		return fail("dot_original_request_conflict")
	}
	sub, err := dotSubmission(from, body, owner.RequestID, owner.SessionID)
	if err != nil {
		return fail("dot_original_request_required")
	}
	session := owner.RequestID
	if explicit := sessionOf(r.Header); explicit != "" {
		session = explicit
	}
	if "sess-"+dotHashParts(owner.Scope, session) != owner.SessionID {
		return fail("dot_request_identity_conflict")
	}
	digest, err := dotbridge.Digest(sub)
	if err != nil || digest != owner.Digest {
		return fail("dot_request_identity_conflict")
	}
	cfg, conn, err := dotbridge.RecoveryPairingSnapshot()
	if err != nil || owner.Scope != dotbridge.PairingIdentity(cfg)+":"+callerScope {
		return fail("dot_original_pairing_required")
	}
	job, _, err := conn.Resume(r.Context(), owner.Scope, owner.RequestID)
	if err != nil {
		return fail(dotErrUnknown)
	}
	if code := dotJobError(job); code != "" {
		return fail(code)
	}
	if job.State != dotbridge.StateCompleted {
		return fail(dotErrPending)
	}
	answer, err := dotAnswer(from, job)
	if err != nil {
		return fail(dotErrFailed)
	}
	w.Header().Set("Content-Type", "application/json")
	if streamOf(body) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		wholeAsStream(w, from, answer)
	} else {
		w.WriteHeader(200)
		_, _ = w.Write(answer)
	}
	return true
}
