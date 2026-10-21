package gateway

import (
	"github.com/yetone/magpie/internal/dotbridge"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDotGetExactInternalIdentity(t *testing.T) {
	for _, header := range []string{"", "stable"} {
		t.Run(header, func(t *testing.T) {
			h := newDotBridgeHarness()
			h.state = dotbridge.StateCompleted
			h.text = "same job"
			setupDotBarrierGateway(t, h)
			w := dotHandlerCall(New(), header, "hello", false)
			if w.Code != 200 {
				t.Fatal(w.Body)
			}
			records, err := dotbridge.RequestArchive()
			if err != nil || len(records) != 1 {
				t.Fatalf("%v %v", records, err)
			}
			req := httptest.NewRequest("GET", "/v1/magpie/dot/requests/"+records[0].RequestID, nil)
			got := httptest.NewRecorder()
			New().Handler().ServeHTTP(got, req)
			if got.Code != 200 || !strings.Contains(got.Body.String(), "same job") || h.posts.Load() != 1 || h.gets.Load() != 1 {
				t.Fatalf("exact ID: %d posts=%d gets=%d %s", got.Code, h.posts.Load(), h.gets.Load(), got.Body)
			}
			req = httptest.NewRequest("GET", "/v1/magpie/dot/requests/"+records[0].RequestID, nil)
			req.Header.Set("Authorization", "Bearer "+TokenFor("codex"))
			got = httptest.NewRecorder()
			New().Handler().ServeHTTP(got, req)
			if got.Code != 404 || h.gets.Load() != 1 || h.posts.Load() != 1 {
				t.Fatalf("cross-agent recovery: %d %s", got.Code, got.Body)
			}
		})
	}
}
