package gateway

import (
	"net/http"
	"strings"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
)

func (s *Server) dotRequest(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/magpie/dot/requests/")
	if id == "" || !dotIDPattern.MatchString(id) {
		writeError(w, provider.Chat, http.StatusBadRequest, "invalid Dot request identity")
		return
	}
	caller := access.Caller(r.Context())
	key := caller.KeyID
	if key == "" {
		key = "local"
	}
	callerScope := dotHashParts(key, callerOf(r).agent)
	records, err := dotbridge.RequestArchive()
	if err != nil {
		writeError(w, provider.Chat, http.StatusConflict, "dot journal unavailable; automatic_redispatch:false")
		return
	}
	var owner *dotbridge.ArchivedRequest
	for i := range records {
		rec := &records[i]
		if rec.Scope != callerScope && !strings.HasSuffix(rec.Scope, ":"+callerScope) {
			continue
		}
		if rec.RequestID == id && rec.BlockReplay {
			owner = rec
			break
		}
	}
	if owner == nil {
		writeError(w, provider.Chat, http.StatusNotFound, "dot request not found")
		return
	}
	if who, held := keyHolds(r); held && !who.Allows(dotModelID(owner.Mode)) {
		writeError(w, provider.Chat, http.StatusForbidden, "Dot recovery is outside the gateway key's models")
		return
	}
	if _, held := accountHolds(r); held {
		writeError(w, provider.Chat, http.StatusForbidden, "Dot recovery is outside the gateway key's accounts")
		return
	}
	cfg, conn, err := dotbridge.RecoveryPairingSnapshot()
	if err != nil || owner.Scope != dotbridge.PairingIdentity(cfg)+":"+callerScope {
		dotFailure(w, nil, provider.Chat, owner.Snapshot, "dot_original_pairing_required")
		return
	}
	job, _, err := conn.Resume(r.Context(), owner.Scope, owner.RequestID)
	if err != nil {
		dotFailure(w, nil, provider.Chat, owner.Snapshot, dotErrUnknown)
		return
	}
	if code := dotJobError(job); code != "" {
		dotFailure(w, nil, provider.Chat, owner.Snapshot, code, job)
		return
	}
	if job.State != dotbridge.StateCompleted {
		dotFailure(w, nil, provider.Chat, owner.Snapshot, dotErrPending, job)
		return
	}
	answer, err := dotAnswer(provider.Chat, job)
	if err != nil {
		dotFailure(w, nil, provider.Chat, owner.Snapshot, dotErrFailed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(answer)
}
