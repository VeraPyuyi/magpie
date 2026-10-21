package gateway

import (
	"encoding/json"
	"github.com/yetone/magpie/internal/provider"
	"net/http/httptest"
	"testing"
)

func TestDotCatalogDoesNotPromiseCallerTools(t *testing.T) {
	h := newDotBridgeHarness()
	setupDotBarrierGateway(t, h)
	if e := provider.SaveGroup(provider.Group{ID: "vendor-first", Name: "Vendor first", Members: []string{"b/m", "dot/task"}, Routing: provider.Ordered}); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/v1/models", CodexCatalogPath} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+TokenFor("cursor-local"))
		w := httptest.NewRecorder()
		New().Handler().ServeHTTP(w, r)
		var out struct {
			Data   []map[string]any `json:"data"`
			Models []map[string]any `json:"models"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		if path == "/v1/models" {
			for _, v := range out.Data {
				id, _ := v["id"].(string)
				if id == "dot/task" || id == "dot/text" || id == "group/vendor-first" || id == "group/dotmix" {
					c := v["capabilities"].(map[string]any)
					if c["supports_tool_use"] != false {
						t.Errorf("Dot claims caller tools: %v", v)
					}
				}
				if id == "b/m" {
					if v["capabilities"].(map[string]any)["supports_tool_use"] != true {
						t.Error("vendor tools lost")
					}
				}
			}
		} else {
			for _, v := range out.Models {
				if v["slug"] == "dot/task" || v["slug"] == "dot/text" || v["slug"] == "group/vendor-first" || v["slug"] == "group/dotmix" {
					t.Errorf("Dot in Codex local-tool catalog: %v", v)
				}
			}
		}
	}
}
