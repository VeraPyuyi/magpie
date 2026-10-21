package gui

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

func TestDotGroupConfigurationWhileUnavailable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := Handler(nil, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/groups", nil))
	var st groupsJSON
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range st.Models {
		if m.ID == "dot/text" {
			found = true
			if !m.Unavailable || m.Reason == "" {
				t.Fatalf("missing reason %+v", m)
			}
		}
	}
	if !found {
		t.Fatal("unpaired Dot missing from group configuration picker")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/groups/save", strings.NewReader(`{"id":"dot-only","name":"Dot only","members":["dot/text"]}`)))
	if w.Code != 200 {
		t.Fatalf("save %d %s", w.Code, w.Body)
	}
	if _, _, ok := provider.Resolve("group/dot-only"); ok {
		t.Fatal("display/configuration authorized execution")
	}
}
