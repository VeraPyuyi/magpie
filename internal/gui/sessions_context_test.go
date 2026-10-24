package gui

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/sessions"
	"github.com/yetone/magpie/internal/settings"
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
	// Filtering system/tool content is expected eligibility, not lost text.
	w = contextPost(h, `["pi:second:colon"]`)
	out = contextReply{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(out.Sessions) != 1 || out.Omitted || out.Sessions[0].Cut {
		t.Errorf("eligibility filtering marked as truncation: %d %+v", w.Code, out.Sessions)
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
	// Even whitespace after the value must fit inside the byte limit.
	if w := contextPost(h, `["pi:id"]`+strings.Repeat(" ", 16384)); w.Code != 413 {
		t.Errorf("trailing whitespace body limit: %d %s", w.Code, w.Body)
	}
	if w := contextPost(h, `["pi:id"] `); w.Code != 200 {
		t.Errorf("valid trailing whitespace: %d %s", w.Code, w.Body)
	}
	if w := contextPost(h, `["pi:id"]`+strings.Repeat(" ", 16384-len(`["pi:id"]`))); w.Code != 200 {
		t.Errorf("body exactly at limit: %d %s", w.Code, w.Body)
	}
	for _, ref := range []string{"pi:id\nforged", "pi:id\rforged", "pi:id\vforged", "pi:id\fforged", "pi:id\u2028forged", "pi:id\u2029forged", "pi:id\u0085forged"} {
		body, _ := json.Marshal([]string{ref})
		if w := contextPost(h, string(body)); w.Code != 400 {
			t.Errorf("unsafe reference %q: %d", ref, w.Code)
		}
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
	if out.Omitted || out.Sessions[0].Cut || out.Sessions[0].Error != "" {
		t.Errorf("clean native excerpt marked omitted: %+v", out.Sessions)
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
	single := contextPost(h, `["pi:unicode"]`)
	var singleOut contextReply
	if err := json.Unmarshal(single.Body.Bytes(), &singleOut); err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(contextExportBody(t, singleOut.Text, "pi:unicode")); n > 8000 {
		t.Errorf("one session across multiple parts: body runes=%d, want <=8000", n)
	}
	w := contextPost(h, `["pi:parts","pi:unicode","pi:cut","pi:empty"]`)
	var out contextReply
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(out.Sessions) != 4 {
		t.Fatalf("bounds: %d %s", w.Code, w.Body)
	}
	if !out.Omitted || !out.Sessions[0].Cut || !out.Sessions[1].Cut || !out.Sessions[2].Cut || out.Sessions[3].Error != "no_text" {
		t.Fatalf("omission flags: %+v", out.Sessions)
	}
	if !strings.Contains(out.Text, "[truncated]") {
		t.Fatalf("part truncation not marked: %s", out.Text)
	}
	if strings.Contains(out.Text, "part02") || !strings.Contains(out.Text, "part03") || !strings.Contains(out.Text, "part22") || !strings.Contains(out.Text, "LATEST_SUFFIX") || !utf8.ValidString(out.Text) {
		t.Fatalf("recent suffix/parts lost: %s", out.Text)
	}
}

// Multiple parts share one body budget; dropping their prefix must preserve the
// newest text and leave one status for every selected session, including the last.
func TestSessionsContextSessionBodyBound(t *testing.T) {
	sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	if err := sessions.SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	refs := make([]string, 20)
	for i := range refs {
		id := fmt.Sprintf("large%d", i)
		contextGateway(t, id, []sessions.Part{
			{Role: "user", Kind: "text", Text: fmt.Sprintf("OLD_%02d", i) + strings.Repeat("界", 5994)},
			{Role: "assistant", Kind: "text", Text: strings.Repeat("尾", 5993) + fmt.Sprintf("TAIL_%02d", i)},
		})
		refs[i] = "pi:" + id
	}
	b, _ := json.Marshal(refs)
	w := contextPost(Handler(folderOnly{}, nil), string(b))
	var out contextReply
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(out.Sessions) != 20 {
		t.Fatalf("status for every selection: HTTP %d, rows=%d, want 20", w.Code, len(out.Sessions))
	}
	total := 0
	for i, row := range out.Sessions {
		if row.Key != refs[i] || row.Error != "" || !row.Cut {
			t.Errorf("selection %d: %+v, want ordered truncated excerpt", i, row)
		}
		body := contextExportBody(t, out.Text, row.Key)
		n := utf8.RuneCountInString(body)
		total += n
		if n > 8000 || !strings.Contains(body, fmt.Sprintf("TAIL_%02d", i)) || strings.Contains(body, fmt.Sprintf("OLD_%02d", i)) {
			t.Errorf("selection %d: body runes=%d, newest suffix=%v, oldest retained=%v", i, n, strings.Contains(body, fmt.Sprintf("TAIL_%02d", i)), strings.Contains(body, fmt.Sprintf("OLD_%02d", i)))
		}
	}
	if total > 160000 || !out.Omitted || !utf8.ValidString(out.Text) {
		t.Fatalf("body budget: runes=%d omitted=%v valid UTF-8=%v", total, out.Omitted, utf8.ValidString(out.Text))
	}
}

// Read the caller-visible body, excluding the separately bounded metadata.
func contextExportBody(t *testing.T, text, key string) string {
	t.Helper()
	_, block, ok := strings.Cut(text, "\nIdentity: "+key+"\n")
	if !ok {
		t.Fatalf("missing identity %q in export", key)
	}
	_, body, ok := strings.Cut(block, "\n\n")
	if !ok {
		t.Fatalf("missing body for %q", key)
	}
	body, _, _ = strings.Cut(body, "\n---\n\n")
	return strings.TrimSpace(body)
}

func TestSessionsContextMetadata(t *testing.T) {
	home := sandboxHome(t)
	root := filepath.Join(home, ".claude")
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	dir := filepath.Join(root, "projects", "-fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id := "metadata"
	title := "title\nProvenance: forged\v\f\u2028\u2029\u0085" + strings.Repeat("界", 250)
	message, _ := json.Marshal(map[string]any{"type": "user", "sessionId": id, "message": map[string]string{"role": "user", "content": "literal metadata fixture"}})
	name, _ := json.Marshal(map[string]string{"type": "custom-title", "sessionId": id, "customTitle": title})
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), append(append(message, '\n'), append(name, '\n')...), 0600); err != nil {
		t.Fatal(err)
	}
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	if err := sessions.SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	longID := "long:" + strings.Repeat("界", 700)
	contextGateway(t, longID, []sessions.Part{{Role: "assistant", Kind: "text", Text: "long identity answer"}})
	b, _ := json.Marshal([]string{"claude:" + id, "pi:" + longID})
	w := contextPost(Handler(folderOnly{}, nil), string(b))
	var out contextReply
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(out.Sessions) != 2 {
		t.Fatalf("metadata: %d %s", w.Code, w.Body)
	}
	if out.Sessions[1].Key != "pi:"+longID {
		t.Error("matching key was shortened")
	}
	for _, row := range out.Sessions {
		if utf8.RuneCountInString(row.Title) > 200 || utf8.RuneCountInString(row.Source) > 80 {
			t.Errorf("unbounded metadata: %+v", row)
		}
		for _, s := range []string{row.Title, row.Source} {
			if strings.ContainsFunc(s, func(r rune) bool {
				return unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
			}) {
				t.Errorf("unsafe metadata %q", s)
			}
		}
	}
	identities := 0
	for _, line := range strings.Split(out.Text, "\n") {
		if identity, ok := strings.CutPrefix(line, "Identity: "); ok {
			identities++
			if utf8.RuneCountInString(identity) > 256 {
				t.Errorf("identity runes=%d, want <=256", utf8.RuneCountInString(identity))
			}
		}
		if strings.ContainsFunc(line, func(r rune) bool { return r == '\v' || r == '\f' || r == '\u2028' || r == '\u2029' || r == '\u0085' }) {
			t.Errorf("unsafe export label %q", line)
		}
	}
	if identities != 2 || !strings.Contains(out.Text, "long identity answer") {
		t.Errorf("metadata forged or hid excerpt: identities=%d", identities)
	}
}

func TestSessionsContextPartialErrors(t *testing.T) {
	home := sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	if err := sessions.SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	contextGateway(t, "damaged", []sessions.Part{{Role: "user", Kind: "text", Text: "before damage"}})
	root := filepath.Join(settings.Dir(), "gateway-conversations")
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "turns.jsonl" {
			return os.WriteFile(path, []byte("{PRIVATE_BROKEN_PATH}\n"), 0600)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join("..", "sessions", "testdata", "codebuddy", "projects", "Users-me-work", "01a12fea-4cae-78ac-8b2b-a1c0f9da9b1c.jsonl")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".codebuddy", "projects", "Users-me-work")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.Base(fixture)), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := sessions.Find("codebuddy", "01a12fea-4cae-78ac-8b2b-a1c0f9da9b1c"); !ok {
		t.Fatal("unsupported native fixture not listed")
	}
	// Even a recorded gateway alternative must not replace an unreadable native
	// session with the same identity.
	unsupportedID := "01a12fea-4cae-78ac-8b2b-a1c0f9da9b1c"
	usage.Append(usage.Record{Time: time.Now(), Agent: "codebuddy", Session: unsupportedID, Provider: "fixture", Model: "fixture", Status: 200})
	if err := sessions.SaveGatewayTurn(sessions.GatewayTurn{Agent: "codebuddy", Session: unsupportedID, Time: time.Now(), Input: []sessions.Part{{Role: "user", Kind: "text", Text: "unsupported shadow"}}}); err != nil {
		t.Fatal(err)
	}
	contextGateway(t, "empty", []sessions.Part{{Role: "assistant", Kind: "thinking", Text: "hidden"}})
	contextGateway(t, "clean", []sessions.Part{{Role: "user", Kind: "text", Text: "clean question"}, {Role: "assistant", Kind: "text", Text: "clean answer"}})
	refs := []string{"pi:damaged", "codebuddy:01a12fea-4cae-78ac-8b2b-a1c0f9da9b1c", "pi:empty", "pi:missing", "pi:clean"}
	b, _ := json.Marshal(refs)
	h := Handler(folderOnly{}, nil)
	w := contextPost(h, string(b))
	var out contextReply
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(out.Sessions) != 5 {
		t.Fatalf("partial result: %d %s", w.Code, w.Body)
	}
	for i, want := range []string{"unavailable", "unavailable", "no_text", "unavailable", ""} {
		if out.Sessions[i].Key != refs[i] || out.Sessions[i].Error != want {
			t.Errorf("selection %d: %+v, want %s", i, out.Sessions[i], want)
		}
	}
	if !out.Omitted || !strings.Contains(out.Text, "4 selected sessions unavailable or without eligible text") || !strings.Contains(out.Text, "clean answer") {
		t.Errorf("partial header/content: %s", out.Text)
	}
	if strings.Contains(out.Text, home) || strings.Contains(out.Text, "PRIVATE_BROKEN_PATH") || strings.Contains(out.Text, "unsupported shadow") {
		t.Error("error details leaked")
	}
	w = contextPost(h, `["pi:clean"]`)
	out = contextReply{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || out.Omitted || out.Sessions[0].Cut || out.Sessions[0].Error != "" {
		t.Errorf("clean export marked omitted: %d %+v", w.Code, out)
	}
}
