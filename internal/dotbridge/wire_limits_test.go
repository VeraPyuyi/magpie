package dotbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Reconstruct the audit's completed_result_html_256kib recipe directly as wire
// bytes, independently of Job/Result serialization. This is a synthetic producer.
func dotWireGolden() string {
	return `{"version":1,"job_id":"job.` + strings.Repeat("d", 32) + `","request_id":"req-` + strings.Repeat("a", 64) + `","request_digest":"` + strings.Repeat("e", 64) + `","mode":"answer","session_id":"sess-` + strings.Repeat("b", 64) + `","state":"completed","created_at":"2026-10-19T12:00:00Z","updated_at":"2026-10-19T12:00:00Z","cancel_requested":false,"result":{"text":"` + strings.Repeat(`\u003c`, 262144) + "\"}}\n"
}

func dotWireAttempt(t *testing.T) *Attempt {
	t.Helper()
	a := &Attempt{}
	if err := a.restore(Snapshot{RequestID: "req-" + strings.Repeat("a", 64), Digest: strings.Repeat("e", 64), Mode: "answer", SessionID: "sess-" + strings.Repeat("b", 64), State: attemptAcceptanceUnknown, BlockReplay: true}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestDotWireGoldenEscapedResult(t *testing.T) {
	golden := dotWireGolden()
	sum := sha256.Sum256([]byte(golden))
	if len(golden) != 1573333 || hex.EncodeToString(sum[:]) != "6cd295dbeecbc815cf1ff9c0f07292203851d2e5dae17b32cb9274eaa5dd7e5a" {
		t.Fatal("audit recipe reconstruction changed")
	}
	for _, artifacts := range []bool{false, true} {
		t.Run(fmt.Sprint(artifacts), func(t *testing.T) {
			body := golden
			wantText := strings.Repeat("<", 262144)
			if artifacts {
				// Artifact bytes share the producer's aggregate 256 KiB result budget.
				body = strings.Replace(body, strings.Repeat(`\u003c`, 40)+`"}}`, `","artifacts":[{"url":"https://example.com/a","label":"a"}]}}`, 1)
				wantText = strings.Repeat("<", 262144-40)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/dot-bridge/v1/requests/req-"+strings.Repeat("a", 64) || r.Header.Get("Authorization") != "Bearer wire-test" {
					t.Error("unexpected request")
				}
				_, _ = io.WriteString(w, body)
			}))
			defer srv.Close()
			conn := newTestConnection(t, srv, "wire-test")
			a := dotWireAttempt(t)
			job, err := conn.ByRequest(context.Background(), a)
			if err != nil {
				t.Fatal(err)
			}
			if job.Result == nil || job.Result.Text != wantText || job.State != StateCompleted || a.Snapshot().State != attemptAccepted {
				t.Fatal("whole verified answer not delivered")
			}
			if artifacts && (len(job.Result.Artifacts) != 1 || job.Result.Artifacts[0].URL != "https://example.com/a" || job.Result.Artifacts[0].Label != "a") {
				t.Fatal("artifacts lost")
			}
			// The larger envelope must still enforce the receipt identity check.
			wrong := dotWireAttempt(t)
			wrong.digest = strings.Repeat("f", 64)
			if _, err := conn.ByRequest(context.Background(), wrong); !errors.Is(err, errReceipt) {
				t.Fatalf("identity mismatch: %v", err)
			}
		})
	}
}

func TestDotWireResponseBoundary(t *testing.T) {
	for _, size := range []int{2 << 20, (2 << 20) + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			// A complete, valid object precedes padding: accepting a truncated prefix
			// would incorrectly succeed on the over-limit case.
			base := dotWireGolden()
			body := base + strings.Repeat(" ", size-len(base))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
			defer srv.Close()
			conn := newTestConnection(t, srv, "wire-test")
			_, err := conn.ByRequest(context.Background(), dotWireAttempt(t))
			if size == 2<<20 && err != nil {
				t.Fatalf("exact envelope must fit: %v", err)
			}
			if size > 2<<20 && !errors.Is(err, errResponseSize) {
				t.Fatalf("over limit: %v", err)
			}
		})
	}
}

func TestDotWireRequestEscaping(t *testing.T) {
	for _, char := range []string{"<", ">", "&", "\x00", "\x01"} {
		t.Run(fmt.Sprintf("%x", char), func(t *testing.T) {
			sub := Submission{RequestID: strings.Repeat("r", 128), SessionID: strings.Repeat("s", 128), Mode: ModeAnswer}
			roles := []string{"system", "user", "assistant"}
			for i := 0; i < 32; i++ {
				sub.Messages = append(sub.Messages, Message{Role: roles[i%3], Content: strings.Repeat(char, 262144/32)})
			}
			var posts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				b, err := io.ReadAll(r.Body)
				if err != nil || len(b) > 2<<20 || len(b) <= 1<<20 {
					t.Errorf("encoded request length %d: %v", len(b), err)
				}
				// Independently construct the expected bytes, including maximal IDs and roles.
				parts := make([]string, 32)
				escaped := map[string]string{"<": `\u003c`, ">": `\u003e`, "&": `\u0026`, "\x00": `\u0000`, "\x01": `\u0001`}[char]
				for i := range parts {
					parts[i] = `{"role":"` + roles[i%3] + `","content":"` + strings.Repeat(escaped, 8192) + `"}`
				}
				want := `{"request_id":"` + sub.RequestID + `","mode":"answer","session_id":"` + sub.SessionID + `","messages":[` + strings.Join(parts, ",") + `],"task":""}`
				if string(b) != want {
					t.Error("request differs from independently specified bytes")
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"code":"invalid_request"}}`)
			}))
			defer srv.Close()
			a, err := NewAttempt(sub)
			if err != nil {
				t.Fatal(err)
			}
			_, err = newTestConnection(t, srv, "wire-test").Submit(context.Background(), sub, a)
			if err == nil || posts.Load() != 1 || !a.Snapshot().BlockReplay {
				t.Fatal("valid request must POST once and retain rejection barrier")
			}
		})
	}
}

func TestDotWireInvalidRequestNoBarrier(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts.Add(1) }))
	defer srv.Close()
	conn := newTestConnection(t, srv, "wire-test")
	for _, rawSize := range []int{262145, 400000} {
		for _, enqueue := range []bool{false, true} {
			sub := testSubmission()
			a, err := NewAttempt(sub)
			if err != nil {
				t.Fatal(err)
			}
			before := a.Snapshot()
			sub.Messages[0].Content = strings.Repeat("\x00", rawSize)
			if enqueue {
				_, err = conn.Enqueue(context.Background(), sub, "wire-invalid", a)
			} else {
				_, err = conn.Submit(context.Background(), sub, a)
			}
			if !errors.Is(err, errInvalidBody) || a.Snapshot() != before || posts.Load() != 0 {
				t.Fatalf("invalid request changed barrier or posted: %v %+v", err, a.Snapshot())
			}
		}
	}
}

func TestDotWireRejectedSubmitKeepsBarrier(t *testing.T) {
	for _, code := range []string{"invalid_request", "conflict", "bridge_paused", "bridge_disabled", "access_revoked", "unknown", "future_code"} {
		t.Run(code, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, `{"error":{"code":%q}}`, code)
			}))
			defer srv.Close()
			sub := testSubmission()
			a, err := NewAttempt(sub)
			if err != nil {
				t.Fatal(err)
			}
			_, err = newTestConnection(t, srv, "wire-test").Submit(context.Background(), sub, a)
			if err == nil || !a.Snapshot().BlockReplay || a.Snapshot().State != attemptAcceptanceUnknown {
				t.Fatal("rejection must remain conservative")
			}
		})
	}
}
