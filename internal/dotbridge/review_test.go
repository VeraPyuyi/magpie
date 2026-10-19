package dotbridge

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDotReviewSubmissionBounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sub   Submission
		valid bool
	}{
		{"exact combined", Submission{"r", "task", "s", []Message{{"user", strings.Repeat("界", 87381)}}, "x"}, true},
		{"over combined", Submission{"r", "task", "s", []Message{{"user", strings.Repeat("界", 87381)}}, "xx"}, false},
		{"invalid content UTF8", Submission{"r", "answer", "s", []Message{{"user", string([]byte{255})}}, ""}, false},
		{"invalid task UTF8", Submission{"r", "task", "s", nil, string([]byte{255})}, false},
		{"empty answer", Submission{"r", "answer", "s", nil, ""}, false},
		{"blank task", Submission{"r", "task", "s", nil, " \n"}, false},
		{"optional session", Submission{"r", "task", "", nil, "work"}, true},
		{"developer", Submission{"r", "answer", "s", []Message{{"developer", "x"}}, ""}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewAttempt(tc.sub)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

func TestDotReviewPorts(t *testing.T) {
	for _, p := range []string{"0", "abc", "65536", "-1", "+80"} {
		if _, err := NewConnection("127.0.0.1:"+p, func() (string, error) { return "t", nil }); err == nil {
			t.Errorf("accepted invalid port %s", p)
		}
	}
}

func TestDotReviewTransport(t *testing.T) {
	var redirects, sockets atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.Close {
			t.Error("keepalive not disabled")
		}
		if r.Header.Get("Authorization") != "Bearer paired" {
			t.Error("missing dedicated bearer")
		}
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	c, err := NewConnection(strings.TrimPrefix(srv.URL, "http://"), func() (string, error) { return "paired", nil })
	if err != nil {
		t.Fatal(err)
	}
	tr := c.client.Transport.(*http.Transport)
	if tr.Proxy != nil || !tr.DisableKeepAlives || c.client.Timeout != 10*time.Second {
		t.Fatal("production transport settings")
	}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		sockets.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	_, _ = c.Status(context.Background())
	if redirects.Load() != 0 || sockets.Load() != 1 {
		t.Fatalf("redirects=%d sockets=%d", redirects.Load(), sockets.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Status(ctx)
	if err == nil {
		t.Fatal("cancelled budget ignored")
	}
}

func TestDotReviewReadyCache(t *testing.T) {
	now := time.Now().UTC()
	status := map[string]any{"version": 1, "enabled": true, "paired": true, "ready": true, "code": "ready", "verified_at": now.Add(-time.Second), "expires_at": now.Add(time.Minute), "subscription": map[string]any{"state": "active", "expires_at": now.Add(5 * time.Second)}, "capabilities": map[string]bool{"text": true, "tasks": false}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(status) }))
	defer srv.Close()
	c := newTestConnection(t, srv, "paired")
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !CachedReady(now.Add(time.Second)) {
		t.Fatal("verified authenticated status did not populate cache")
	}
	if CachedReady(now.Add(6 * time.Second)) {
		t.Fatal("subscription expiry ignored")
	}
	if CachedReady(now.Add(-time.Hour)) {
		t.Fatal("future cache accepted")
	}
	status["ready"] = false
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if CachedReady(time.Now()) {
		t.Fatal("unready status did not invalidate cache")
	}
}

func TestDotReviewWireEmptyMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		if string(body["messages"]) != "[]" {
			t.Errorf("messages=%s", body["messages"])
		}
		w.WriteHeader(400)
	}))
	defer srv.Close()
	sub := Submission{RequestID: "r", Mode: "task", SessionID: "s", Task: "work"}
	a, _ := NewAttempt(sub)
	_, _ = newTestConnection(t, srv, "paired").Submit(context.Background(), sub, a)
}
