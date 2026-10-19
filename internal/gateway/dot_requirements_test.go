package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

func setupDotGateway(t *testing.T, bridgePosts *atomic.Int32) (*httptest.Server, *httptest.Server) {
	t.Helper()
	noRests()
	t.Cleanup(noRests)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	enableDotBridgeForGateway(t)
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/dot-bridge/v1/jobs" {
			bridgePosts.Add(1)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"accepted":true,"version":1,"job_id":"job-1","request_id":"req-test","request_digest":"d","mode":"answer","session_id":"sess-test","state":"queued"}`))
	}))
	t.Cleanup(bridge.Close)
	fakeB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(`event: response.completed`+"\n"+`data: {"type":"response.completed","response":{"id":"r1","model":"m1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}}`))
	}))
	t.Cleanup(fakeB.Close)
	if err := provider.Save(provider.Provider{ID: "b", Name: "B", Key: "k", Models: []string{"m1"}, Responses: fakeB.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{ID: "dotmix", Name: "dotmix", Members: []string{"dot/text", "b/m1"}, Routing: provider.Ordered}); err != nil {
		t.Fatal(err)
	}
	return bridge, fakeB
}

func TestDotCapabilitiesBeforeSubmit(t *testing.T) {
	var bridgePosts atomic.Int32
	setupDotGateway(t, &bridgePosts)
	body := `{"model":"group/dotmix","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],"tools":[{"type":"function","name":"x"}]}`
	req, err := dotRequirementsOf(provider.Responses, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	g, ms, ok := provider.FindGroup("group/dotmix")
	if !ok || len(ms) < 2 {
		t.Fatalf("group members=%v ok=%v", ms, ok)
	}
	cs, pl := New().planGroup(g, ms, provider.Responses)
	if len(cs) < 2 {
		t.Fatalf("planned=%d", len(cs))
	}
	filtered, _ := dotCandidates(cs, pl, req)
	if len(filtered) != 1 || filtered[0].p.ID != "b" {
		t.Fatalf("filtered=%v", filtered)
	}
	if bridgePosts.Load() != 0 {
		t.Fatalf("dot POSTs=%d", bridgePosts.Load())
	}
}

func TestDotExplicitUnsupportedNoFallback(t *testing.T) {
	var bridgePosts atomic.Int32
	setupDotGateway(t, &bridgePosts)
	h := New().Handler()
	body := `{"model":"dot/text","stream":false,"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","name":"x"}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	h.ServeHTTP(rec, req)
	if bridgePosts.Load() != 0 {
		t.Fatalf("dot POSTs=%d", bridgePosts.Load())
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tools") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestDotRequirementsDeveloperRole(t *testing.T) {
	_, err := dotChatMessages([]byte(`[{"role":"developer","content":"x"},{"role":"user","content":"hi"}]`))
	if err == nil || !strings.Contains(err.Error(), "developer") {
		t.Fatalf("err=%v", err)
	}
}

func TestDotSubmissionPreservesRoles(t *testing.T) {
	body := `{"model":"dot/text","messages":[{"role":"system","content":"s"},{"role":"user","content":"u"},{"role":"assistant","content":"a"}]}`
	sub, err := dotSubmission(provider.Chat, []byte(body), "req-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"system", "user", "assistant"}
	for i, role := range want {
		if sub.Messages[i].Role != role {
			t.Fatalf("messages=%v", sub.Messages)
		}
	}
	b, _ := json.Marshal(sub)
	if strings.Contains(string(b), "developer") {
		t.Fatal("developer must not appear")
	}
}
