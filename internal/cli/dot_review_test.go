package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/settings"
)

func TestDotReviewPairAuthenticatedBoundary(t *testing.T) {
	for _, code := range []int{200, 401} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			store := dotSandbox(t)
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if store.writes != 0 {
					t.Error("vault write before authentication")
				}
				if r.Header.Get("Authorization") != "Bearer sentinel-private-token" {
					t.Error("missing pairing authorization")
				}
				w.WriteHeader(code)
				json.NewEncoder(w).Encode(dotbridge.Status{Version: 1, Paired: true})
			}))
			defer srv.Close()
			f := dotPairingFile{Version: 1, ClientID: "operator", QueueID: "operator", Token: "sentinel-private-token", Listen: strings.TrimPrefix(srv.URL, "http://"), CreatedAt: time.Now()}
			b, _ := json.Marshal(f)
			p := dotTempFile(t, "pair.json")
			dotWriteFile(t, p, b)
			dotTestPrivate(t, p)
			var out bytes.Buffer
			err := dotTo(context.Background(), &out, []string{"pair", p})
			if calls.Load() != 1 {
				t.Fatalf("authentication not reached: calls=%d err=%v", calls.Load(), err)
			}
			cfgBytes, readErr := os.ReadFile(filepath.Join(settings.Dir(), "dot-bridge.json"))
			if code == 200 {
				if err != nil || store.writes != 1 || store.token != f.Token || readErr != nil {
					t.Fatal(err, store.writes, readErr)
				}
				cfg, e := dotbridge.LoadConfig()
				if e != nil || !cfg.Enabled || cfg.Credential != "dot-cli:operator" || cfg.Address != f.Listen {
					t.Fatal(cfg, e)
				}
				if strings.Contains(string(cfgBytes), f.Token) {
					t.Fatal("token persisted in config")
				}
				if _, e := os.Stat(p); e != nil {
					t.Fatal("pair file removed")
				}
			} else if err == nil || store.writes != 0 || !os.IsNotExist(readErr) {
				t.Fatal("authentication failure wrote state", err, store.writes, readErr)
			}
			if strings.Contains(out.String(), f.Token) || err != nil && strings.Contains(err.Error(), f.Token) {
				t.Fatal("secret leaked")
			}
		})
	}
}

func TestDotReviewStrictInputs(t *testing.T) {
	for _, body := range []string{`{"mode":"task","mode":"answer"}`, `{"Mode":"task"}`, `null`, `[]`, `{} {}`, `{"unknown":1}`, `{"messages":[{"role":"user","Role":"system"}]}`} {
		var sub dotbridge.Submission
		if err := dotDecode([]byte(body), &sub); err == nil {
			t.Errorf("accepted ambiguous JSON %s", body)
		}
	}
	for _, body := range []string{`{"listen":"127.0.0.1:1","listen":"127.0.0.1:2"}`, `{"Token":"secret"}`} {
		var f dotPairingFile
		if err := dotDecode([]byte(body), &f); err == nil {
			t.Errorf("accepted ambiguous pairing JSON %s", body)
		}
	}
}

func TestDotReviewInvalidReceipts(t *testing.T) {
	for _, cmd := range []string{"job", "cancel"} {
		for _, body := range []string{`{}`, `{"version":1,"job_id":"other-job","state":"completed","result":{"text":"fabricated"}}`, `{"version":1,"job_id":"job-1","request_id":"r1","request_digest":"abc","mode":"task","state":"succeeded"}`, `{"version":1,"job_id":"job-1","request_id":"r1","request_digest":"abc","mode":"task","state":"running","result":{"text":"fabricated"}}`} {
			t.Run(cmd+body, func(t *testing.T) {
				dotSandbox(t)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
				defer srv.Close()
				if err := dotbridge.SaveConfig(dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(srv.URL, "http://"), Credential: "cli"}); err != nil {
					t.Fatal(err)
				}
				var out bytes.Buffer
				err := dotTo(context.Background(), &out, []string{cmd, "job-1"})
				if err == nil || out.Len() != 0 {
					t.Fatalf("unvalidated receipt emitted: %v %s", err, out.String())
				}
			})
		}
	}
}

func TestDotReviewPairInvalidBoundary(t *testing.T) {
	// Cases that must be rejected after reaching the authenticated status
	// endpoint, with an otherwise-valid private pairing fixture.
	for _, tc := range []struct {
		why    string
		status func(http.ResponseWriter)
	}{
		{"unpaired", dotStatusUnpaired},
		{"bad-version", dotStatusBadVersion},
	} {
		t.Run(tc.why, func(t *testing.T) {
			store := dotSandbox(t)
			var calls atomic.Int32
			status := func(w http.ResponseWriter) {
				calls.Add(1)
				tc.status(w)
			}
			path := dotPairFixture(t, dotPreparePrivate, nil, status)
			var out bytes.Buffer
			err := dotTo(context.Background(), &out, []string{"pair", path})
			if err == nil {
				t.Fatalf("%s: import was accepted", tc.why)
			}
			if store.writes != 0 {
				t.Fatalf("%s: %d vault writes", tc.why, store.writes)
			}
			if out.Len() != 0 {
				t.Fatalf("%s: printed output: %s", tc.why, out.String())
			}
			if _, statErr := os.Stat(filepath.Join(settings.Dir(), "dot-bridge.json")); !os.IsNotExist(statErr) {
				t.Fatalf("%s: config was written: %v", tc.why, statErr)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("%s: status calls=%d, want 1", tc.why, got)
			}
		})
	}
	// Cases that must be rejected before any HTTP request, on a private fixture.
	for _, tc := range []struct {
		why       string
		transform func(*dotPairingFile, string)
		body      []byte
	}{
		{"nonloopback", func(f *dotPairingFile, _ string) { f.Listen = "192.0.2.1:1234" }, nil},
		{"url", func(f *dotPairingFile, raw string) { f.Listen = raw }, nil},
		{"duplicate", nil, []byte(`{"listen":"127.0.0.1:1","version":1,"client_id":"operator","queue_id":"operator","token":"sentinel-private-token","listen":"127.0.0.1:2","created_at":"2026-10-19T00:00:00Z"}`)},
		{"case", nil, []byte(`{"Version":1,"ClientID":"operator","QueueID":"operator","Token":"sentinel-private-token","Listen":"127.0.0.1:1","CreatedAt":"2026-10-19T00:00:00Z"}`)},
		{"unknown", nil, []byte(`{"unknown":true,"version":1,"client_id":"operator","queue_id":"operator","token":"sentinel-private-token","listen":"127.0.0.1:1","created_at":"2026-10-19T00:00:00Z"}`)},
		{"trailing", nil, []byte(`{"version":1,"client_id":"operator","queue_id":"operator","token":"sentinel-private-token","listen":"127.0.0.1:1","created_at":"2026-10-19T00:00:00Z"} {}`)},
		{"null", nil, []byte(`null`)},
	} {
		t.Run(tc.why, func(t *testing.T) {
			dotPairRejected(t, tc.why, 0, tc.transform, tc.body, dotPreparePrivate)
		})
	}
}

func TestDotReviewJournalAndCancelIdentity(t *testing.T) {
	for _, kind := range []string{"request", "digest", "mode", "session", "cancel-wrong-id", "cancel-empty", "cancel-no-intent", "valid"} {
		t.Run(kind, func(t *testing.T) {
			dotSandbox(t)
			var posts atomic.Int32
			sub := dotbridge.Submission{RequestID: "r1", Mode: dotbridge.ModeTask, SessionID: "s1", Task: "Explicit task"}
			digest, _ := dotbridge.Digest(sub)
			original := dotbridge.Job{Version: 1, JobID: "j1", RequestID: "r1", RequestDigest: digest, Mode: sub.Mode, SessionID: "s1", State: dotbridge.StateRunning}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				j := original
				if r.Method == "POST" {
					posts.Add(1)
					j.CancelRequested = true
					switch kind {
					case "cancel-wrong-id":
						j.JobID = "j2"
					case "cancel-empty":
						j = dotbridge.Job{}
					case "cancel-no-intent":
						j.CancelRequested = false
					}
				} else {
					switch kind {
					case "request":
						j.RequestID = "foreign"
					case "digest":
						j.RequestDigest = strings.Repeat("a", 64)
					case "mode":
						j.Mode = dotbridge.ModeAnswer
					case "session":
						j.SessionID = "foreign"
					}
				}
				json.NewEncoder(w).Encode(j)
			}))
			defer srv.Close()
			cfg := dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(srv.URL, "http://"), Credential: "cli"}
			if err := dotbridge.SaveConfig(cfg); err != nil {
				t.Fatal(err)
			}
			snap, _, err := dotbridge.Reserve(sub, dotScope(cfg))
			if err != nil {
				t.Fatal(err)
			}
			snap.JobID = "j1"
			if err := dotbridge.SaveAttempt(dotScope(cfg), snap); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = dotTo(context.Background(), &out, []string{"cancel", "j1"})
			if kind == "valid" {
				if err != nil || !strings.Contains(out.String(), "intent") {
					t.Fatal(err, out.String())
				}
				return
			}
			if err == nil || out.Len() != 0 {
				t.Fatal("identity mismatch acknowledged", err, out.String())
			}
			want := int32(0)
			if strings.HasPrefix(kind, "cancel-") {
				want = 1
			}
			if posts.Load() != want {
				t.Fatal("unexpected POST", posts.Load(), want)
			}
		})
	}
}

func TestDotReviewTaskStrictCLI(t *testing.T) {
	dotSandbox(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	dotbridge.SaveConfig(dotbridge.Config{Enabled: true, Address: strings.TrimPrefix(srv.URL, "http://"), Credential: "cli"})
	for _, body := range []string{`{"request_id":"r","mode":"task","mode":"answer","task":"x"}`, `{"request_id":"r","Mode":"task","task":"x"}`, `null`, `[]`, `{} {}`, `{"unknown":1}`, `{"messages":[{"role":"user","Role":"system"}]}`} {
		p := dotTempFile(t, "task.json")
		dotWriteFile(t, p, []byte(body))
		dotTestPrivate(t, p)
		var out bytes.Buffer
		if err := dotTo(context.Background(), &out, []string{"submit", "--file", p}); err == nil || out.Len() != 0 {
			t.Fatal("ambiguous task accepted", body, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid task reached HTTP", calls.Load())
	}
}
