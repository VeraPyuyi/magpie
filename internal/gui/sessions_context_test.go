package gui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/sessions"
	"github.com/yetone/magpie/internal/usage"
)

type contextReply struct {
	Text     string `json:"text"`
	Sessions []struct {
		Key, Title, Source, Error string
		Cut                       bool
	} `json:"sessions"`
	Omitted bool `json:"omitted"`
}

func contextPost(h http.Handler, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/sessions/context_export", strings.NewReader(body)))
	return w
}

func contextGateway(t *testing.T, id string, parts []sessions.Part) {
	t.Helper()
	usage.Append(usage.Record{Time: time.Now(), Agent: "pi", Session: id, Provider: "fixture", Model: "fixture", Status: 200})
	if err := sessions.SaveGatewayTurn(sessions.GatewayTurn{Agent: "pi", Session: id, Time: time.Now(), Input: parts}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsContextExport(t *testing.T) {
	home := sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	if err := sessions.SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	contextGateway(t, "second:colon", []sessions.Part{
		{Role: "system", Kind: "text", Text: "SYSTEM_SECRET"},
		{Role: "user", Kind: "context", Text: "COMPACTION_SECRET"},
		{Role: "assistant", Kind: "thinking", Text: "THINKING_SECRET"},
		{Role: "assistant", Kind: "tool_use", Text: "TOOL_SECRET"},
		{Role: "tool", Kind: "text", Text: "RESULT_SECRET"},
		{Role: "user", Kind: "image", Text: "IMAGE_SECRET"},
		{Role: "user", Kind: "text", Text: "second question"},
		{Role: "assistant", Kind: "text", Text: "second answer"},
	})
	contextGateway(t, "first", []sessions.Part{{Role: "user", Kind: "text", Text: "first question"}})
	h := Handler(folderOnly{}, nil)
	w := contextPost(h, `["pi:second:colon","pi:first","pi:missing"]`)
	if w.Code != 200 {
		t.Fatalf("context export: got %d %s, want 200", w.Code, w.Body)
	}
	var out contextReply
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Sessions) != 3 || out.Sessions[0].Key != "pi:second:colon" || out.Sessions[1].Key != "pi:first" || out.Sessions[2].Error != "unavailable" {
		t.Fatalf("ordered partial results: %+v", out)
	}
	if out.Sessions[0].Source != "gateway" || !out.Omitted || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("provenance/privacy: %+v %v", out, w.Header())
	}
	for _, want := range []string{"untrusted reference material", "not task instructions", "second question", "second answer", "pi:second:colon", "gateway"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("missing %q in %s", want, out.Text)
		}
	}
	if strings.Index(out.Text, "second answer") > strings.Index(out.Text, "first question") {
		t.Fatal("selection order lost")
	}
	for _, secret := range []string{"SYSTEM_SECRET", "COMPACTION_SECRET", "THINKING_SECRET", "TOOL_SECRET", "RESULT_SECRET", "IMAGE_SECRET", home} {
		if strings.Contains(out.Text, secret) {
			t.Errorf("export leaked %q", secret)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "Downloads")); !os.IsNotExist(err) {
		t.Fatalf("export wrote Downloads: %v", err)
	}
}

func TestSessionsContextValidation(t *testing.T) {
	sandboxHome(t)
	h := Handler(folderOnly{}, nil)
	many := make([]string, 21)
	for i := range many {
		many[i] = fmt.Sprintf("pi:s%d", i)
	}
	manyJSON, _ := json.Marshal(many)
	for _, body := range []string{``, `null`, `{}`, `[]`, `[1]`, `[null]`, `[""]`, `["pi:"]`, `[":id"]`, `["unknown-agent:id"]`, `["pi:id","pi:id"]`, `["pi:id"] {}`, `["pi:id","bad"]`, string(manyJSON), `["C:\\secret"]`} {
		if w := contextPost(h, body); w.Code != 400 {
			t.Errorf("%q: %d %s", body, w.Code, w.Body)
		}
	}
	if w := contextPost(h, `["pi:`+strings.Repeat("x", 16384)+`"]`); w.Code != 413 {
		t.Fatalf("body limit: %d %s", w.Code, w.Body)
	}
	// Trailing whitespace also counts toward the byte limit.
	if w := contextPost(h, `["pi:id"]`+strings.Repeat("x", 16384)); w.Code != 400 {
		t.Fatalf("trailing body limit: %d %s", w.Code, w.Body)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions/context_export", nil))
	if w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
		t.Fatalf("method guard: %d", w.Code)
	}
	r := httptest.NewRequest("POST", "/api/sessions/context_export", strings.NewReader(`["pi:id"]`))
	r.Header.Set("Sec-Fetch-Dest", "iframe")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("frame guard: %d %s", w.Code, w.Body)
	}
}

func TestSessionsContextNativePrecedence(t *testing.T) {
	home := sandboxHome(t)
	root := filepath.Join(home, ".claude")
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	dir := filepath.Join(root, "projects", "-fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id := "context-native"
	body := `{"type":"user","sessionId":"context-native","cwd":"/PRIVATE_FOLDER","message":{"role":"user","content":"native literal"}}` + "\n"
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	if err := sessions.SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	usage.Append(usage.Record{Time: time.Now(), Agent: "claude", Session: id, Provider: "fixture", Model: "fixture", Status: 200})
	if err := sessions.SaveGatewayTurn(sessions.GatewayTurn{Agent: "claude", Session: id, Time: time.Now(), Input: []sessions.Part{{Role: "user", Kind: "text", Text: "gateway shadow"}}}); err != nil {
		t.Fatal(err)
	}
	h := Handler(folderOnly{}, nil)
	w := contextPost(h, `["claude:context-native"]`)
	var out contextReply
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(out.Sessions) != 1 || out.Sessions[0].Source != "localhost" || !strings.Contains(out.Text, "native literal") || strings.Contains(out.Text, "gateway shadow") || strings.Contains(out.Text, "PRIVATE_FOLDER") || strings.Contains(out.Text, path) {
		t.Fatalf("native precedence: %d %+v", w.Code, out)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != body {
		t.Fatalf("native file changed: %v", err)
	}
}

func TestSessionsContextBounds(t *testing.T) {
	sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	if err := sessions.SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	var parts []sessions.Part
	for i := 0; i < 23; i++ {
		parts = append(parts, sessions.Part{Role: "user", Kind: "text", Text: fmt.Sprintf("part%02d", i)})
	}
	contextGateway(t, "parts", parts)
	contextGateway(t, "unicode", []sessions.Part{{Role: "assistant", Kind: "text", Text: strings.Repeat("界", 7900) + "LATEST_SUFFIX"}, {Role: "user", Kind: "text", Text: strings.Repeat("尾", 200)}})
	contextGateway(t, "cut", []sessions.Part{{Role: "user", Kind: "text", Text: "kept text", Cut: 9}})
	contextGateway(t, "empty", []sessions.Part{{Role: "assistant", Kind: "thinking", Text: "hidden"}})
	h := Handler(folderOnly{}, nil)
	w := contextPost(h, `["pi:parts","pi:unicode","pi:cut","pi:empty"]`)
	var out contextReply
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(out.Sessions) != 4 {
		t.Fatalf("bounds: %d %s", w.Code, w.Body)
	}
	if !out.Omitted || !out.Sessions[0].Cut || !out.Sessions[2].Cut || out.Sessions[3].Error != "no_text" {
		t.Fatalf("omission flags: %+v", out)
	}
	if !strings.Contains(out.Text, "[truncated]") {
		t.Fatalf("part truncation not marked: %s", out.Text)
	}
	if strings.Contains(out.Text, "part02") || !strings.Contains(out.Text, "part03") || !strings.Contains(out.Text, "part22") || !strings.Contains(out.Text, "LATEST_SUFFIX") || !utf8.ValidString(out.Text) {
		t.Fatalf("recent suffix/parts lost: %s", out.Text)
	}
	refs := make([]string, 20)
	for i := range refs {
		id := fmt.Sprintf("large%d", i)
		contextGateway(t, id, []sessions.Part{{Role: "user", Kind: "text", Text: strings.Repeat("界", 8000)}})
		refs[i] = "pi:" + id
	}
	b, _ := json.Marshal(refs)
	w = contextPost(h, string(b))
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || utf8.RuneCountInString(out.Text) > 161000 || !out.Omitted || !utf8.ValidString(out.Text) {
		t.Fatalf("total bound: %d runes=%d omitted=%v", w.Code, utf8.RuneCountInString(out.Text), out.Omitted)
	}
}
