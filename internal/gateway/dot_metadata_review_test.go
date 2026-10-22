package gateway

import (
	"encoding/json"
	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDotCompletedMetadataWholeAndStream(t *testing.T) {
	for _, p := range []provider.Protocol{provider.Chat, provider.Responses} {
		j := dotbridge.Job{Mode: dotbridge.ModeTask, State: dotbridge.StateCompleted, Summary: "done", RequestID: "req-internal", JobID: "job-id", Result: &dotbridge.Result{Text: "result", Artifacts: []dotbridge.Artifact{{URL: "https://example.com/a", Label: "file"}}}}
		b, e := dotAnswer(p, j)
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]any
		json.Unmarshal(b, &v)
		var m any = v["magpie_dot"]
		if p == provider.Chat {
			m = v["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["magpie_dot"]
		}
		if m == nil {
			t.Fatalf("metadata missing %s", b)
		}
		w := httptest.NewRecorder()
		wholeAsStream(w, p, b)
		for _, s := range []string{`"magpie_dot"`, `"state":"completed"`, `"mode":"task"`, `"summary":"done"`, `"request_id":"req-internal"`, `"job_id":"job-id"`, `/v1/magpie/dot/requests/req-internal`, `"artifacts"`} {
			if !strings.Contains(w.Body.String(), s) {
				t.Errorf("stream missing %s: %s", s, w.Body)
			}
		}
	}
}
func TestDotPendingMetadataUsesJobProgress(t *testing.T) {
	h := newDotBridgeHarness()
	h.state = dotbridge.StateWaitingUser
	setupDotBarrierGateway(t, h)
	w := dotHandlerCall(New(), "pending-progress", "hello", false)
	for _, s := range []string{`"state":"waiting_user"`, `"mode":"answer"`, `"status_url":"/v1/magpie/dot/requests/`} {
		if !strings.Contains(w.Body.String(), s) {
			t.Errorf("pending missing %s: %s", s, w.Body)
		}
	}
}
