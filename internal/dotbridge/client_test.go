package dotbridge

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testSubmission() Submission {
	return Submission{
		RequestID: "req-test1",
		Mode:      ModeAnswer,
		SessionID: "sess-test1",
		Messages:  []Message{{Role: "user", Content: "hello"}},
		Task:      "",
	}
}

func testJob(sub Submission, digest string) Job {
	now := time.Now().UTC().Truncate(time.Second)
	return Job{
		Version:       1,
		JobID:         "job-1",
		RequestID:     sub.RequestID,
		RequestDigest: digest,
		Mode:          sub.Mode,
		SessionID:     sub.SessionID,
		State:         StateQueued,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

type bridgeServer struct {
	posts      atomic.Int32
	gets       atomic.Int32
	authHeader string
	job        Job
	storeJob   bool
	dropAfter  bool
	malformed  bool
	badDigest  bool
	notFound   bool
}

func (s *bridgeServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != s.authHeader {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Legacy-Token") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/dot-bridge/v1/jobs":
			s.posts.Add(1)
			if s.dropAfter && s.storeJob {
				h, ok := w.(http.Hijacker)
				if !ok {
					panic("fixture requires hijack")
				}
				conn, _, err := h.Hijack()
				if err != nil {
					panic(err)
				}
				_ = conn.Close()
				return
			}
			var sub Submission
			_ = json.NewDecoder(r.Body).Decode(&sub)
			digest, _ := Digest(sub)
			job := s.job
			if job.RequestDigest == "" {
				job = testJob(sub, digest)
			}
			if s.badDigest {
				job.RequestDigest = "deadbeef"
			}
			if s.malformed {
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte("{"))
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(submitEnvelope{Job: job, Accepted: true})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/dot-bridge/v1/requests/"):
			s.gets.Add(1)
			if s.notFound {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(s.job)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newTestConnection(t *testing.T, srv *httptest.Server, token string) *Connection {
	host := strings.TrimPrefix(srv.URL, "http://")
	conn, err := NewConnection(host, func() (string, error) { return token, nil })
	if err != nil {
		t.Fatal(err)
	}
	transport := conn.client.Transport.(*http.Transport)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	return conn
}

func TestDotClientReceipt(t *testing.T) {
	sub := testSubmission()
	digest, _ := Digest(sub)
	bs := &bridgeServer{authHeader: "Bearer paired-token", job: testJob(sub, digest)}
	srv := httptest.NewServer(bs.handler())
	t.Cleanup(srv.Close)
	conn := newTestConnection(t, srv, "paired-token")
	attempt, err := NewAttempt(sub)
	if err != nil {
		t.Fatal(err)
	}
	job, err := conn.Submit(context.Background(), sub, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if job.JobID != "job-1" || job.RequestID != sub.RequestID {
		t.Fatalf("unexpected job %+v", job)
	}
	snap := attempt.Snapshot()
	if !snap.BlockReplay || snap.State != attemptAccepted {
		t.Fatalf("expected accepted barrier, got %+v", snap)
	}
	if bs.posts.Load() != 1 {
		t.Fatalf("expected one POST, got %d", bs.posts.Load())
	}
}

func TestDotClientUncertainSubmit(t *testing.T) {
	sub := testSubmission()
	digest, _ := Digest(sub)
	job := testJob(sub, digest)
	bs := &bridgeServer{authHeader: "Bearer paired-token", job: job, storeJob: true, dropAfter: true}
	srv := httptest.NewServer(bs.handler())
	t.Cleanup(srv.Close)
	conn := newTestConnection(t, srv, "paired-token")
	attempt, _ := NewAttempt(sub)
	_, err := conn.Submit(context.Background(), sub, attempt)
	if err == nil {
		t.Fatal("expected error after dropped response")
	}
	snap := attempt.Snapshot()
	if snap.State != attemptAcceptanceUnknown || !snap.BlockReplay {
		t.Fatalf("barrier must stay unknown: %+v", snap)
	}
	bs.dropAfter = false
	recovered, err := conn.ByRequest(context.Background(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.JobID != job.JobID {
		t.Fatalf("expected same job %s got %s", job.JobID, recovered.JobID)
	}
	if bs.posts.Load() != 1 {
		t.Fatalf("expected exactly one POST, got %d", bs.posts.Load())
	}
}

func TestDotClientMalformedReceipt(t *testing.T) {
	sub := testSubmission()
	bs := &bridgeServer{authHeader: "Bearer paired-token", malformed: true}
	srv := httptest.NewServer(bs.handler())
	t.Cleanup(srv.Close)
	conn := newTestConnection(t, srv, "paired-token")
	attempt, _ := NewAttempt(sub)
	_, err := conn.Submit(context.Background(), sub, attempt)
	if err == nil {
		t.Fatal("expected malformed receipt error")
	}
	snap := attempt.Snapshot()
	if snap.State != attemptAcceptanceUnknown || !snap.BlockReplay {
		t.Fatal("malformed 202 must not clear barrier")
	}
}

func TestDotClientDigestMismatchReceipt(t *testing.T) {
	sub := testSubmission()
	bs := &bridgeServer{authHeader: "Bearer paired-token", badDigest: true}
	srv := httptest.NewServer(bs.handler())
	t.Cleanup(srv.Close)
	conn := newTestConnection(t, srv, "paired-token")
	attempt, _ := NewAttempt(sub)
	_, err := conn.Submit(context.Background(), sub, attempt)
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
	snap := attempt.Snapshot()
	if snap.State != attemptAcceptanceUnknown || !snap.BlockReplay {
		t.Fatal("digest mismatch must not clear barrier")
	}
}

func TestDotClient404AfterUncertain(t *testing.T) {
	sub := testSubmission()
	bs := &bridgeServer{authHeader: "Bearer paired-token", notFound: true}
	srv := httptest.NewServer(bs.handler())
	t.Cleanup(srv.Close)
	conn := newTestConnection(t, srv, "paired-token")
	attempt, _ := NewAttempt(sub)
	attempt.beginSubmit()
	_, err := conn.ByRequest(context.Background(), attempt)
	if err == nil {
		t.Fatal("expected 404")
	}
	snap := attempt.Snapshot()
	if !snap.BlockReplay {
		t.Fatal("404 after uncertain POST leaves BlockReplay true")
	}
}

func TestDotClientAddressAndAuth(t *testing.T) {
	posts := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
	}))
	t.Cleanup(srv.Close)

	for _, addr := range []string{"example.com:1234", "http://127.0.0.1:9", "127.0.0.1", "0.0.0.0:1"} {
		conn, err := NewConnection(addr, func() (string, error) { return "t", nil })
		if err == nil {
			t.Fatalf("expected address error for %q", addr)
		}
		if conn != nil {
			_, _ = conn.Submit(context.Background(), testSubmission(), &Attempt{})
		}
	}
	if posts.Load() != 0 {
		t.Fatalf("non-loopback must make zero requests, got %d", posts.Load())
	}

	bs := &bridgeServer{authHeader: "Bearer paired-token"}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != bs.authHeader {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		bs.posts.Add(1)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(submitEnvelope{Accepted: false})
	}))
	t.Cleanup(srv2.Close)
	conn := newTestConnection(t, srv2, "paired-token")
	attempt, _ := NewAttempt(testSubmission())
	_, err := conn.Submit(context.Background(), testSubmission(), attempt)
	if err == nil {
		t.Fatal("expected receipt error without accepted job")
	}
	if bs.posts.Load() != 1 {
		t.Fatalf("expected dedicated bearer POST")
	}
	if conn.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("proxy must be nil")
	}
}

func TestDotClientEnqueueSameDigestJob(t *testing.T) {
	sub := testSubmission()
	digest, _ := Digest(sub)
	bs := &bridgeServer{authHeader: "Bearer paired-token", job: testJob(sub, digest)}
	srv := httptest.NewServer(bs.handler())
	t.Cleanup(srv.Close)
	SetJournalPath(t.TempDir() + "/journal.json")
	conn := newTestConnection(t, srv, "paired-token")
	attempt, _ := NewAttempt(sub)
	job1, err := conn.Enqueue(context.Background(), sub, "scope-a", attempt)
	if err != nil {
		t.Fatal(err)
	}
	attempt2, _ := NewAttempt(sub)
	job2, err := conn.Enqueue(context.Background(), sub, "scope-a", attempt2)
	if err != nil {
		t.Fatal(err)
	}
	if job1.JobID != job2.JobID {
		t.Fatal("same id+content should resolve same job")
	}
	if bs.posts.Load() != 1 {
		t.Fatalf("second enqueue must not POST again, posts=%d", bs.posts.Load())
	}
}

func TestDotClientConcurrentReserve(t *testing.T) {
	sub := testSubmission()
	SetJournalPath(t.TempDir() + "/journal.json")
	var wg sync.WaitGroup
	results := make([]bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, existing, err := Reserve(sub, "scope-b")
			if err != nil {
				t.Error(err)
				return
			}
			results[idx] = existing
		}(i)
	}
	wg.Wait()
	var newCount int
	for _, existing := range results {
		if !existing {
			newCount++
		}
	}
	if newCount != 1 {
		t.Fatalf("expected one new identity, got %d new results", newCount)
	}
}
