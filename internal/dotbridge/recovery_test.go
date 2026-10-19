package dotbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDotResumeChildProcess(t *testing.T) {
	if path := os.Getenv("DOT_CLIENT_CHILD_JOURNAL"); path != "" {
		SetJournalPath(path)
		c, err := NewConnection(os.Getenv("DOT_CLIENT_CHILD_ADDRESS"), func() (string, error) { return "child-paired", nil })
		if err != nil {
			t.Fatal(err)
		}
		job, a, err := c.Resume(context.Background(), "child-scope", "req-test1")
		if err != nil {
			t.Fatal(err)
		}
		if job.JobID != "job-1" || !a.Snapshot().BlockReplay {
			t.Fatal("child failed to recover verified receipt")
		}
		return
	}
	path := filepath.Join(t.TempDir(), "journal.json")
	SetJournalPath(path)
	sub := testSubmission()
	if _, _, err := Reserve(sub, "child-scope"); err != nil {
		t.Fatal(err)
	}
	digest, _ := Digest(sub)
	var posts, gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			posts.Add(1)
			w.WriteHeader(405)
			return
		}
		gets.Add(1)
		if r.URL.Path != "/dot-bridge/v1/requests/req-test1" || r.Header.Get("Authorization") != "Bearer child-paired" {
			w.WriteHeader(403)
			return
		}
		_ = json.NewEncoder(w).Encode(testJob(sub, digest))
	}))
	defer srv.Close()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestDotResumeChildProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "DOT_CLIENT_CHILD_JOURNAL="+path, "DOT_CLIENT_CHILD_ADDRESS="+strings.TrimPrefix(srv.URL, "http://"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, output)
	}
	if posts.Load() != 0 || gets.Load() != 1 {
		t.Fatalf("posts=%d gets=%d", posts.Load(), gets.Load())
	}
}

func TestDotConcurrentRestoreAndJournalPaths(t *testing.T) {
	sub := testSubmission()
	a, _ := NewAttempt(sub)
	snap := a.Snapshot()
	path := filepath.Join(t.TempDir(), "journal.json")
	SetJournalPath(path)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = a.restore(snap)
				_ = a.Snapshot()
				SetJournalPath(path)
				_ = journalPath()
			}
		}()
	}
	wg.Wait()
}
