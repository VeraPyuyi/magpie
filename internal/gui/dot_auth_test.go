package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/gateway"
)

// Exercise the same webHandler/ownWindow boundaries used by the production
// hosts, not a context or authentication shortcut into the Dot routes.
func TestDotGUIProductionAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	dotbridge.SetCredentialStore(dotGUIVault{})
	dotbridge.SetJournalPath(filepath.Join(t.TempDir(), "journal.json"))
	t.Cleanup(func() { dotbridge.SetCredentialStore(nil); dotbridge.SetJournalPath("") })
	sub := dotbridge.Submission{RequestID: "auth-request", SessionID: "auth-session", Mode: "task", Task: "explicit fixture"}
	digest, err := dotbridge.Digest(sub)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer gui-fixture-secret" {
			t.Error("paired authentication missing")
			w.WriteHeader(401)
			return
		}
		job := dotbridge.Job{Version: 1, JobID: "auth-job", RequestID: sub.RequestID, SessionID: sub.SessionID, RequestDigest: digest, Mode: "task", State: "running"}
		switch {
		case r.URL.Path == "/dot-bridge/v1/status":
			now := time.Now()
			json.NewEncoder(w).Encode(dotbridge.Status{Version: 1, Enabled: true, Paired: true, Ready: true, VerifiedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Subscription: dotbridge.SubscriptionStatus{State: "active", ExpiresAt: now.Add(time.Hour)}, Capabilities: dotbridge.Capabilities{Tasks: true}})
		case r.Method == "POST" && r.URL.Path == "/dot-bridge/v1/jobs":
			job.State = "queued"
			w.WriteHeader(202)
			json.NewEncoder(w).Encode(struct {
				dotbridge.Job
				Accepted bool `json:"accepted"`
			}{job, true})
		case r.URL.Path == "/dot-bridge/v1/requests/auth-request":
			json.NewEncoder(w).Encode(job)
		case r.URL.Path == "/dot-bridge/v1/jobs/auth-job/cancel":
			job.State = "cancel_requested"
			job.CancelRequested = true
			json.NewEncoder(w).Encode(job)
		default:
			w.WriteHeader(404)
		}
	}))
	defer bridge.Close()
	cfg := dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(bridge.URL, "http://"), Credential: "auth-fixture", AnswerWaitSeconds: 120}
	if err := dotbridge.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err = dotbridge.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := dotbridge.Reserve(sub, dotGUIScope(cfg)); err != nil {
		t.Fatal(err)
	}
	key := "fixture-gui-session-key"
	web := gateway.AllowGuard(webHandler("3999", key, 0, Handler(webHost{}, nil)))
	native := ownWindow(Handler(nil, nil))
	body, _ := json.Marshal(sub)
	endpoints := []struct {
		method, path string
		want         int
	}{{"GET", "status", 200}, {"GET", "requests", 200}, {"GET", "requests/auth-request", 200}, {"POST", "jobs", 202}, {"POST", "requests/auth-request/cancel", 200}, {"POST", "off", 200}}
	for _, host := range []string{"web", "native"} {
		for _, endpoint := range endpoints {
			t.Run(host+"/"+endpoint.path, func(t *testing.T) {
				cases := []struct {
					name, cookie, origin, peer, requestHost string
					headers                                 map[string]string
					want                                    int
				}{
					{name: "authorized", cookie: key, want: endpoint.want},
					{name: "foreign-origin", cookie: key, origin: "http://evil.invalid", want: 403},
					{name: "null-origin", cookie: key, origin: "null", want: 403},
					{name: "wrong-scheme", cookie: key, origin: "https://127.0.0.1:3999", want: 403},
					{name: "wrong-port", cookie: key, origin: "http://127.0.0.1:4000", want: 403},
					{name: "foreign-host", cookie: key, requestHost: "evil.invalid", want: 403},
					{name: "frame", cookie: key, headers: map[string]string{"Sec-Fetch-Dest": "iframe"}, want: 403},
					{name: "cross-site", cookie: key, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: 403},
					{name: "forwarded", cookie: key, headers: map[string]string{"Forwarded": "for=127.0.0.1"}, want: 403},
					{name: "forwarded-for", cookie: key, headers: map[string]string{"X-Forwarded-For": "127.0.0.1"}, want: 403},
					{name: "forwarded-host", cookie: key, headers: map[string]string{"X-Forwarded-Host": "localhost"}, want: 403},
					{name: "forwarded-proto", cookie: key, headers: map[string]string{"X-Forwarded-Proto": "http"}, want: 403},
					{name: "bearer", cookie: key, headers: map[string]string{"Authorization": "Bearer gateway-caller-key"}, want: 403},
				}
				if host == "web" {
					cases = append(cases, struct {
						name, cookie, origin, peer, requestHost string
						headers                                 map[string]string
						want                                    int
					}{name: "missing-cookie", want: 401}, struct {
						name, cookie, origin, peer, requestHost string
						headers                                 map[string]string
						want                                    int
					}{name: "wrong-cookie", cookie: "wrong", want: 401}, struct {
						name, cookie, origin, peer, requestHost string
						headers                                 map[string]string
						want                                    int
					}{name: "LAN-local-session", cookie: key, peer: "192.168.1.5:1234", want: 403}, struct {
						name, cookie, origin, peer, requestHost string
						headers                                 map[string]string
						want                                    int
					}{name: "LAN-bearer", cookie: key, peer: "192.168.1.5:1234", headers: map[string]string{"Authorization": "Bearer gateway-caller-key"}, want: 403}, struct {
						name, cookie, origin, peer, requestHost string
						headers                                 map[string]string
						want                                    int
					}{name: "forged-native", cookie: key, requestHost: "wails.localhost", headers: map[string]string{"X-Window-Scheme": "http", "Origin": "http://wails.localhost"}, want: 403})
				}
				for _, tc := range cases {
					t.Run(tc.name, func(t *testing.T) {
						if err := dotbridge.SaveConfig(cfg); err != nil {
							t.Fatal(err)
						}
						baseline, err := dotbridge.LoadConfig()
						if err != nil {
							t.Fatal(err)
						}
						if _, _, err := dotbridge.Reserve(sub, dotGUIScope(baseline)); err != nil {
							t.Fatal(err)
						}
						before := calls.Load()
						r := httptest.NewRequest(endpoint.method, "http://127.0.0.1:3999/api/dot/"+endpoint.path, strings.NewReader(string(body)))
						r.RemoteAddr = "127.0.0.1:4321"
						handler := web
						if host == "native" {
							handler = native
							r.RemoteAddr = "192.0.2.1:1234"
							r.Host = "localhost"
							scheme := "wails"
							if runtime.GOOS == "windows" {
								scheme = "http"
								r.Host = "wails.localhost"
							}
							r.Header.Set("Origin", scheme+"://"+r.Host)
						} else {
							r.Header.Set("Origin", "http://127.0.0.1:3999")
						}
						if tc.cookie != "" {
							r.AddCookie(&http.Cookie{Name: "magpie_web_3999", Value: tc.cookie})
						}
						if tc.origin != "" {
							r.Header.Set("Origin", tc.origin)
						}
						if tc.peer != "" {
							r.RemoteAddr = tc.peer
						}
						if tc.requestHost != "" {
							r.Host = tc.requestHost
						}
						for k, v := range tc.headers {
							r.Header.Set(k, v)
						}
						w := httptest.NewRecorder()
						handler.ServeHTTP(w, r)
						if w.Code != tc.want {
							t.Fatalf("got %d want %d: %s", w.Code, tc.want, w.Body)
						}
						if tc.want >= 400 {
							if calls.Load() != before {
								t.Fatalf("denied operation reached bridge: %d calls", calls.Load()-before)
							}
							after, err := dotbridge.LoadConfig()
							if err != nil || after != baseline {
								t.Fatalf("denied operation changed config: %+v %v", after, err)
							}
						}
					})
				}
			})
		}
	}
}
