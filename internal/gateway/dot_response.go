package gateway

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
)

func dotAnswer(proto provider.Protocol, job dotbridge.Job) ([]byte, error) {
	if job.Result == nil || dotbridge.ValidateResult(job.Result) != nil {
		return nil, errDotNoAnswer
	}
	text := dotRenderText(job.Result)
	if strings.TrimSpace(text) == "" {
		return nil, errDotNoAnswer
	}
	modelID := dotModelID(job.Mode)
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
			"model":   modelID,
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": text, "magpie_dot": dotMetadata(job)}, "finish_reason": "stop"}},
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
			"model":      modelID,
			"output": []map[string]any{{
				"id":      "msg_" + id,
				"type":    "message",
				"role":    "assistant",
				"status":  "completed",
				"content": []map[string]any{{"type": "output_text", "text": text, "annotations": []any{}}},
			}},
			"error":      nil,
			"magpie_dot": dotMetadata(job),
		})
		return b, nil
	default:
		return nil, errDotUnsupportedProto
	}
}

func dotMetadata(job dotbridge.Job) map[string]any {
	summary := job.Summary
	if len(summary) > 2048 {
		summary = ""
	}
	m := map[string]any{"mode": job.Mode, "state": job.State, "summary": summary, "request_id": job.RequestID, "job_id": job.JobID, "status_url": "/v1/magpie/dot/requests/" + job.RequestID}
	if job.Result != nil && dotbridge.ValidateResult(job.Result) == nil {
		m["artifacts"] = job.Result.Artifacts
	}
	return m
}

func dotRenderText(res *dotbridge.Result) string {
	var b strings.Builder
	b.WriteString(res.Text)
	for _, a := range res.Artifacts {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		label := strings.TrimSpace(a.Label)
		if label == "" {
			label = a.URL
		}
		label = strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\r", " ", "\n", " ", "<", "&lt;", ">", "&gt;").Replace(label)
		destination := strings.NewReplacer("(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "\\", "%5C", " ", "%20").Replace(a.URL)
		b.WriteString("[" + label + "](<" + destination + ">)")
	}
	return b.String()
}
