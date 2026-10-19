package gui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
)

// dotLocal is deliberately narrower than sharing the GUI: gateway caller keys
// never grant access to this machine's paired bridge. Native transport identity
// comes from ownWindow's context, never a header.
func dotLocal(r *http.Request) bool {
	for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
		if r.Header.Get(name) != "" {
			return false
		}
	}
	if r.Header.Get("Authorization") != "" {
		return false
	}
	native, _ := r.Context().Value(windowSchemeKey{}).(string)
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	localHost := host == "localhost"
	if ip, err := netip.ParseAddr(host); err == nil {
		localHost = ip.IsLoopback()
	}
	if native != "" {
		localHost = host == "localhost" || host == "wails.localhost"
	}
	if !localHost {
		return false
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	ip, parseErr := netip.ParseAddr(peer)
	if err != nil || parseErr != nil || !ip.IsLoopback() {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.User != nil || u.Host != r.Host || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return false
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if native != "" {
			scheme = native
		}
		if u.Scheme != scheme {
			return false
		}
	}
	return !apiFramed(r)
}

// Pairing is intentionally CLI-only: no browser endpoint accepts credentials,
// paths or pairing JSON. Scope binds GUI requests to the paired machine, separate
// from CLI and gateway journals; only bounded identity snapshots are returned.
func dotGUIScope(cfg dotbridge.Config) string {
	sum := sha256.Sum256([]byte(cfg.Address + "\x00" + cfg.Credential))
	return "gui:" + hex.EncodeToString(sum[:])
}

func dotTaskAvailable(st dotbridge.Status) bool {
	now := time.Now()
	return st.Version == 1 && st.Enabled && st.Paired && st.Ready && st.Capabilities.Tasks && st.Subscription.State == "active" && !st.VerifiedAt.IsZero() && !st.VerifiedAt.After(now) && now.Before(st.VerifiedAt.Add(10*time.Minute)) && now.Before(st.ExpiresAt) && now.Before(st.Subscription.ExpiresAt)
}

func dotReplyError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"code": code})
}

func dotRoutes(mux *http.ServeMux) {
	mux.Handle("/api/dot/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if !dotLocal(r) {
			dotReplyError(w, 403, "local_window_required")
			return
		}
		cfg, err := dotbridge.LoadConfig()
		if err != nil {
			dotReplyError(w, 503, "configuration_unavailable")
			return
		}
		scope := dotGUIScope(cfg)
		path := strings.TrimPrefix(r.URL.Path, "/api/dot/")
		if path == "off" && r.Method == "POST" {
			cfg.Enabled = false
			if dotbridge.SaveConfig(cfg) != nil {
				dotReplyError(w, 503, "configuration_unavailable")
				return
			}
			writeJSON(w, map[string]bool{"enabled": false})
			return
		}
		if path == "requests" && r.Method == "GET" {
			items, err := dotbridge.Requests(scope)
			if err != nil {
				dotReplyError(w, 503, "journal_unavailable")
				return
			}
			if len(items) > 100 {
				items = items[len(items)-100:]
			}
			if items == nil {
				items = []dotbridge.Snapshot{}
			}
			writeJSON(w, items)
			return
		}
		if path == "status" && r.Method == "GET" && !cfg.Enabled {
			writeJSON(w, map[string]any{"enabled": false, "code": "disabled", "task_available": false})
			return
		}
		// No route may create a connection from browser input.
		c, err := dotbridge.PairedConnection()
		if err != nil {
			dotReplyError(w, 503, "unverified")
			return
		}
		if path == "status" && r.Method == "GET" {
			st, err := c.Status(r.Context())
			if err != nil {
				dotReplyError(w, 503, "unverified")
				return
			}
			writeJSON(w, struct {
				dotbridge.Status
				TaskAvailable bool `json:"task_available"`
			}{st, dotTaskAvailable(st)})
			return
		}
		if path == "jobs" && r.Method == "POST" {
			var sub dotbridge.Submission
			d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 300*1024))
			d.DisallowUnknownFields()
			if d.Decode(&sub) != nil || d.Decode(new(any)) != io.EOF || sub.Mode != dotbridge.ModeTask {
				dotReplyError(w, 400, "invalid_task")
				return
			}
			a, err := dotbridge.NewAttempt(sub)
			if err != nil {
				dotReplyError(w, 400, "invalid_task")
				return
			}
			st, err := c.Status(r.Context())
			if err != nil || !dotTaskAvailable(st) {
				dotReplyError(w, 409, "probe_required")
				return
			}
			job, err := c.Enqueue(r.Context(), sub, scope, a)
			if err != nil {
				dotReplyError(w, 409, "outcome_unknown")
				return
			}
			w.WriteHeader(http.StatusAccepted)
			writeJSON(w, job)
			return
		}
		if strings.HasPrefix(path, "requests/") {
			id := strings.TrimPrefix(path, "requests/")
			cancel := strings.HasSuffix(id, "/cancel")
			if cancel {
				id = strings.TrimSuffix(id, "/cancel")
			}
			if (!cancel && r.Method != "GET") || (cancel && r.Method != "POST") {
				dotReplyError(w, 405, "method_not_allowed")
				return
			}
			// Resume validates request/digest/mode/session against this caller's journal.
			job, _, err := c.Resume(r.Context(), scope, id)
			if err != nil {
				dotReplyError(w, 409, "outcome_unknown")
				return
			}
			if cancel {
				receipt, err := c.Cancel(r.Context(), job.JobID)
				// Cancel has no exported validator. Bind the acknowledgement to the
				// job just validated by Resume; never display cancel response results.
				if err != nil || !dotCancelMatches(job, receipt) {
					dotReplyError(w, 409, "outcome_unknown")
					return
				}
				writeJSON(w, map[string]any{"code": "cancel_intent_acknowledged"})
				return
			}
			writeJSON(w, job)
			return
		}
		dotReplyError(w, 404, "not_found")
	}))
}

func dotCancelMatches(a, b dotbridge.Job) bool {
	if b.Version != 1 || a.JobID != b.JobID || a.RequestID != b.RequestID || a.RequestDigest != b.RequestDigest || a.Mode != b.Mode || a.SessionID != b.SessionID || !b.CancelRequested {
		return false
	}
	switch b.State {
	case dotbridge.StateQueued, dotbridge.StateClaimed, dotbridge.StateRunning, dotbridge.StateWaitingUser, dotbridge.StateCancelRequested, dotbridge.StateOutcomeUnknown:
		return b.Result == nil
	case dotbridge.StateCancelled, dotbridge.StateCompleted, dotbridge.StateFailed:
		// Cancellation acknowledgement is intent only; results are obtained by the
		// separate validated Resume query, never trusted from a cancel response.
		return b.Result == nil
	}
	return false
}
