package gateway

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
)

func dotAnswer(proto provider.Protocol, job dotbridge.Job) ([]byte, error) {
	if job.Result == nil || strings.TrimSpace(job.Result.Text) == "" {
		return nil, errDotNoAnswer
	}
	text := job.Result.Text
	id := job.JobID
	if id == "" {
		id = newID()
	}
	switch proto {
	case provider.Chat:
		if !strings.HasPrefix(id, "chatcmpl-") {
			id = "chatcmpl-" + id
		}
		b, _ := json.Marshal(map[string]any{
			"id":      id,
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "dot/text",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}},
		})
		return b, nil
	case provider.Responses:
		if !strings.HasPrefix(id, "resp_") {
			id = "resp_" + id
		}
		b, _ := json.Marshal(map[string]any{
			"id":         id,
			"object":     "response",
			"created_at": time.Now().Unix(),
			"status":     "completed",
			"model":      "dot/text",
			"output": []map[string]any{{
				"id":      "msg_" + id,
				"type":    "message",
				"role":    "assistant",
				"status":  "completed",
				"content": []map[string]any{{"type": "output_text", "text": text, "annotations": []any{}}},
			}},
			"error": nil,
		})
		return b, nil
	default:
		return nil, errDotUnsupportedProto
	}
}
