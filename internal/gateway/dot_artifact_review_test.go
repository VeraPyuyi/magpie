package gateway

import (
	"encoding/json"
	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDotArtifactLabelCannotInjectLink(t *testing.T) {
	for _, proto := range []provider.Protocol{provider.Chat, provider.Responses} {
		job := dotbridge.Job{Mode: dotbridge.ModeTask, JobID: "job-safe", Result: &dotbridge.Result{Artifacts: []dotbridge.Artifact{{Label: "report](http://127.0.0.1/admin) [download\\\n", URL: "https://example.com/report(a)"}}}}
		b, err := dotAnswer(proto, job)
		if err != nil {
			t.Fatal(err)
		}
		check := func(text string) {
			t.Helper()
			if strings.Contains(text, `report](http://127.0.0.1/admin)`) {
				t.Fatalf("injected markdown: %s", text)
			}
			if !strings.Contains(text, `[report\]`) || !strings.Contains(text, `https://example.com/report%28a%29`) {
				t.Fatalf("escaped destination/label missing: %s", text)
			}
		}
		var v map[string]any
		json.Unmarshal(b, &v)
		if proto == provider.Chat {
			check(v["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string))
		} else {
			check(v["output"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string))
		}
		w := httptest.NewRecorder()
		wholeAsStream(w, proto, b)
		found := false
		for _, line := range strings.Split(w.Body.String(), "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var event map[string]any
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) != nil {
				continue
			}
			if proto == provider.Chat {
				choices, _ := event["choices"].([]any)
				if len(choices) > 0 {
					delta, _ := choices[0].(map[string]any)["delta"].(map[string]any)
					if text, ok := delta["content"].(string); ok {
						check(text)
						found = true
					}
				}
			} else if event["type"] == "response.output_text.delta" {
				check(event["delta"].(string))
				found = true
			}
		}
		if !found {
			t.Fatalf("no streamed content: %s", w.Body)
		}
	}
}
