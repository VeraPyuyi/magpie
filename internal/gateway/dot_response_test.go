package gateway

import (
	"encoding/json"
	"testing"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
)

func TestDotAnswerChatOmitsUsage(t *testing.T) {
	b, err := dotAnswer(provider.Chat, dotbridge.Job{Mode: dotbridge.ModeAnswer, JobID: "job-1", Result: &dotbridge.Result{Text: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["usage"]; ok {
		t.Fatalf("usage present: %v", out)
	}
	choices, _ := out["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "hello" {
		t.Fatalf("content=%v", msg["content"])
	}
}

func TestDotAnswerResponsesOmitsUsage(t *testing.T) {
	b, err := dotAnswer(provider.Responses, dotbridge.Job{Mode: dotbridge.ModeAnswer, JobID: "job-1", Result: &dotbridge.Result{Text: "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["usage"]; ok {
		t.Fatalf("usage present: %v", out)
	}
}
