package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/gwmw"
	"github.com/yetone/magpie/internal/provider"
)

// Dot's text-only parser must never reject or rewrite another provider's wire.
// Exercise Handler with Dot both absent and ready, including a non-Dot group
// and an upstream model whose name happens to be text.
func TestDotPreflightLeavesNonDotRequests(t *testing.T) {
	for _, ready := range []bool{false, true} {
		name := "disabled"
		if ready {
			name = "ready"
		}
		t.Run(name, func(t *testing.T) {
			noRests()
			t.Cleanup(noRests)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			if ready {
				enableDotBridgeForGateway(t)
			}
			sent := make(chan []byte, 1)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				sent <- b
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/responses" {
					io.WriteString(w, `{"id":"r1","object":"response","model":"m1","status":"completed","output":[]}`)
				} else {
					io.WriteString(w, chatWholeOK)
				}
			}))
			t.Cleanup(up.Close)
			if err := provider.Save(provider.Provider{ID: "other", Name: "Other", Key: "k", Models: []string{"m1", "text"}, Chat: up.URL + "/v1", Responses: up.URL + "/v1"}); err != nil {
				t.Fatal(err)
			}
			if err := provider.SaveGroup(provider.Group{ID: "ordinary", Name: "ordinary", Members: []string{"other/m1"}, Routing: provider.Ordered}); err != nil {
				t.Fatal(err)
			}
			for _, model := range []string{"other/m1", "other/text", "group/ordinary", "text"} {
				for _, tc := range []struct{ name, path, field, value, extra string }{
					{"chat-tool", "/v1/chat/completions", "messages", `[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"result"},{"role":"user","content":"continue"}]`, ""},
					{"chat-image", "/v1/chat/completions", "messages", `[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGk="}}]}]`, ""},
					{"chat-developer", "/v1/chat/completions", "messages", `[{"role":"developer","content":"be brief"},{"role":"user","content":"hi"}]`, ""},
					{"chat-tools", "/v1/chat/completions", "messages", `[{"role":"user","content":"hi"}]`, `,"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}]`},
					{"responses-calls", "/v1/responses", "input", `[{"type":"function_call","call_id":"call_1","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"result"},{"type":"message","role":"user","content":"continue"}]`, ""},
					{"responses-image", "/v1/responses", "input", `[{"type":"input_image","image_url":"data:image/png;base64,aGk="}]`, ""},
					{"responses-nested-image", "/v1/responses", "input", `[{"type":"message","role":"user","content":[{"type":"input_text","text":"look"},{"type":"input_image","image_url":"data:image/png;base64,aGk="}]}]`, ""},
					{"responses-developer", "/v1/responses", "input", `[{"type":"message","role":"developer","content":"be brief"},{"type":"message","role":"user","content":"hi"}]`, ""},
				} {
					t.Run(model+"/"+tc.name, func(t *testing.T) {
						body := `{"model":"` + model + `","stream":false,"` + tc.field + `":` + tc.value + tc.extra + `}`
						code, reply := postTo(t, New(), tc.path, body)
						if code != http.StatusOK {
							t.Fatalf("status=%d body=%s", code, reply)
						}
						select {
						case b := <-sent:
							var got map[string]json.RawMessage
							if err := json.Unmarshal(b, &got); err != nil {
								t.Fatal(err)
							}
							want := tc.value
							if tc.name == "chat-developer" {
								// The existing Chat forwarder normalizes developer to
								// system for every upstream; Dot must preserve that behavior.
								want = `[{"role":"system","content":"be brief"},{"role":"user","content":"hi"}]`
							}
							sameJSON(t, got[tc.field], want)
							if tc.extra != "" {
								var want map[string]json.RawMessage
								json.Unmarshal([]byte(body), &want)
								sameJSON(t, got["tools"], string(want["tools"]))
							}
						default:
							t.Fatal("non-Dot upstream was not asked")
						}
					})
				}
			}
		})
	}
}

func TestDotPreflightSkipsUnsupportedGroupMember(t *testing.T) {
	for _, tc := range []struct{ name, path, content string }{
		{"tools", "/v1/responses", `"input":"hi","tools":[{"type":"function","name":"read"}]`},
		{"developer", "/v1/responses", `"input":[{"type":"message","role":"developer","content":"be brief"},{"type":"message","role":"user","content":"hi"}]`},
		{"tool-output", "/v1/chat/completions", `"messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"result"},{"role":"user","content":"hi"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posts atomic.Int32
			setupDotGateway(t, &posts)
			s := New()
			code, reply := postTo(t, s, tc.path, `{"model":"group/dotmix","stream":true,`+tc.content+`}`)
			if code != http.StatusOK || posts.Load() != 0 {
				t.Fatalf("status=%d Dot POSTs=%d body=%s", code, posts.Load(), reply)
			}
			rs := s.Trace(t.Context(), 0, 0).Routes
			if len(rs) != 1 || len(rs[0].Tries) != 1 || rs[0].Tries[0].ID != "b" || rs[0].Tries[0].Status != http.StatusOK {
				t.Fatalf("routes=%+v", rs)
			}
		})
	}
}

type dotAddOutput struct{}

func (dotAddOutput) Tried(body []byte, to gwmw.To) []byte {
	if to.ID == provider.DotID {
		return withFields(body, map[string]any{"max_output_tokens": 1000})
	}
	return body
}

func TestDotPreflightChecksFinalCandidateBody(t *testing.T) {
	for _, tc := range []struct{ name, path, member, content string }{
		{"chat-effort", "/v1/chat/completions", "dot/text:high", `"messages":[{"role":"user","content":"hi"}]`},
		{"responses-effort", "/v1/responses", "dot/text:high", `"input":"hi"`},
		{"responses-output", "/v1/responses", "dot/text", `"input":"hi"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posts atomic.Int32
			setupDotGateway(t, &posts)
			if err := provider.SaveGroup(provider.Group{ID: "dotmix", Name: "dotmix", Members: []string{tc.member, "b/m1"}, Routing: provider.Ordered}); err != nil {
				t.Fatal(err)
			}
			if tc.name == "responses-output" {
				if err := provider.SetModelOutput("dot/text", 100); err != nil {
					t.Fatal(err)
				}
			}
			s := New()
			rec := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{"model":"group/dotmix","stream":true,`+tc.content+`}`))
			if tc.name == "responses-output" {
				r = r.WithContext(context.WithValue(r.Context(), triedKey{}, []gwmw.Tried{dotAddOutput{}}))
			}
			s.Handler().ServeHTTP(rec, r)
			if rec.Code != http.StatusOK || posts.Load() != 0 {
				t.Fatalf("status=%d Dot POSTs=%d body=%s", rec.Code, posts.Load(), rec.Body)
			}
			rs := s.Trace(t.Context(), 0, 0).Routes
			if len(rs) != 1 || len(rs[0].Tries) != 1 || rs[0].Tries[0].ID != "b" || rs[0].Tries[0].Status != http.StatusOK {
				t.Fatalf("unsupported Dot candidate reached the attempt: routes=%+v", rs)
			}
		})
	}
}
