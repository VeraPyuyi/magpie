package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
)

func TestDotTaskHandler(t *testing.T) {
	for _, proto := range []provider.Protocol{provider.Chat, provider.Responses} {
		t.Run(string(proto), func(t *testing.T) {
			h := newDotBridgeHarness()
			h.state = dotbridge.StateCompleted
			h.text = "cloud result"
			setupDotBarrierGateway(t, h)
			path, body := "/v1/chat/completions", `{"model":"dot/task","messages":[{"role":"system","content":"provided context"},{"role":"user","content":"research this"}]}`
			if proto == provider.Responses {
				path, body = "/v1/responses", `{"model":"dot/task","instructions":"provided context","input":"research this"}`
			}
			code, b := postTo(t, New(), path, body)
			if code != 200 || !strings.Contains(b, `"model":"dot/task"`) || h.posts.Load() != 1 {
				t.Fatalf("code=%d posts=%d %s", code, h.posts.Load(), b)
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			for _, job := range h.jobs {
				if job.Mode != dotbridge.ModeTask {
					t.Fatalf("job=%+v", job)
				}
			}
		})
	}
}

func TestDotLocalEnvironmentRejected(t *testing.T) {
	for _, content := range []string{
		"<environment_context><cwd>/caller/repo</cwd></environment_context>",
		"<env>\nWorking directory: D:/caller/repo\n</env>",
		"Primary working directory: /caller/repo",
	} {
		req, err := dotRequirementsOf(provider.Responses, []byte(`{"input":`+strconvQuote(content)+`}`))
		if err != nil || req.ok() {
			t.Fatalf("local context admitted: %+v %v", req, err)
		}
	}
	req, err := dotRequirementsOf(provider.Responses, []byte(`{"input":"Explain the public documentation for /v1/models"}`))
	if err != nil || !req.ok() {
		t.Fatalf("ordinary path text rejected: %+v %v", req, err)
	}
}

func TestDotTaskArtifactsOnly(t *testing.T) {
	b, err := dotAnswer(provider.Chat, dotbridge.Job{Mode: dotbridge.ModeTask, JobID: "job-1", Result: &dotbridge.Result{Artifacts: []dotbridge.Artifact{{URL: "https://example.com/a", Label: "a"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "https://example.com/a") {
		t.Fatalf("artifact link missing: %s", b)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out["model"] != "dot/task" {
		t.Fatalf("model=%v", out["model"])
	}
}
