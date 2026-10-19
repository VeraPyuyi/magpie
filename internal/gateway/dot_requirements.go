package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/dotbridge"
	"github.com/yetone/magpie/internal/provider"
)

// dotRequirements lists why a request cannot use the Dot text bridge.
type dotRequirements struct {
	Unsupported []string
}

func (r dotRequirements) ok() bool { return len(r.Unsupported) == 0 }

func dotRequirementsOf(from provider.Protocol, body []byte) (dotRequirements, error) {
	var out dotRequirements
	if from != provider.Chat && from != provider.Responses {
		out.Unsupported = append(out.Unsupported, "protocol "+string(from))
		return out, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return out, fmt.Errorf("invalid request: %v", err)
	}
	allowed := dotAllowedTopLevel(from)
	for k := range raw {
		if !allowed[k] {
			out.Unsupported = append(out.Unsupported, "field "+k)
		}
	}
	if from == provider.Chat {
		if err := dotChatShape(raw, &out); err != nil {
			return out, err
		}
	} else {
		if err := dotResponsesShape(raw, &out); err != nil {
			return out, err
		}
	}
	return out, nil
}

func dotAllowedTopLevel(from provider.Protocol) map[string]bool {
	common := map[string]bool{
		"model": true, "stream": true, "user": true, "metadata": true,
		"prompt_cache_key": true, "store": true, "service_tier": true,
	}
	if from == provider.Chat {
		common["messages"] = true
		return common
	}
	common["input"] = true
	common["instructions"] = true
	return common
}

func dotChatShape(raw map[string]json.RawMessage, out *dotRequirements) error {
	if hasJSON(raw, "tools") {
		out.Unsupported = append(out.Unsupported, "tools")
	}
	if tc, ok := raw["tool_choice"]; ok && !dotJSONNull(tc) {
		out.Unsupported = append(out.Unsupported, "tool_choice")
	}
	if hasJSON(raw, "response_format") {
		out.Unsupported = append(out.Unsupported, "response_format")
	}
	if hasJSON(raw, "web_search_options") {
		out.Unsupported = append(out.Unsupported, "web_search_options")
	}
	if hasJSON(raw, "reasoning_effort") {
		out.Unsupported = append(out.Unsupported, "reasoning_effort")
	}
	if hasJSON(raw, "parallel_tool_calls") {
		out.Unsupported = append(out.Unsupported, "parallel_tool_calls")
	}
	if hasJSON(raw, "max_tokens") || hasJSON(raw, "max_completion_tokens") {
		out.Unsupported = append(out.Unsupported, "max_tokens")
	}
	if hasJSON(raw, "temperature") || hasJSON(raw, "top_p") || hasJSON(raw, "stop") {
		out.Unsupported = append(out.Unsupported, "generation")
	}
	if hasJSON(raw, "access_programs") {
		out.Unsupported = append(out.Unsupported, "access_programs")
	}
	msgs, err := dotChatMessages(raw["messages"])
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		out.Unsupported = append(out.Unsupported, "messages")
		return nil
	}
	if err := dotCheckSubmission(dotbridge.ModeAnswer, msgs, ""); err != nil {
		out.Unsupported = append(out.Unsupported, err.Error())
	}
	return nil
}

func dotResponsesShape(raw map[string]json.RawMessage, out *dotRequirements) error {
	for _, k := range []string{"tools", "tool_choice", "text", "reasoning", "max_output_tokens", "temperature", "top_p", "previous_response_id", "truncation", "modalities", "parallel_tool_calls", "include", "client_metadata", "access_programs", "additional_tools"} {
		if hasJSON(raw, k) {
			out.Unsupported = append(out.Unsupported, k)
		}
	}
	msgs, err := dotResponsesMessages(raw)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		out.Unsupported = append(out.Unsupported, "input")
		return nil
	}
	if err := dotCheckSubmission(dotbridge.ModeAnswer, msgs, ""); err != nil {
		out.Unsupported = append(out.Unsupported, err.Error())
	}
	return nil
}

func dotResponsesMessages(raw map[string]json.RawMessage) ([]dotbridge.Message, error) {
	var msgs []dotbridge.Message
	if ins, ok := raw["instructions"]; ok && !dotJSONNull(ins) {
		var s string
		if err := json.Unmarshal(ins, &s); err != nil || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("instructions")
		}
		msgs = append(msgs, dotbridge.Message{Role: "system", Content: s})
	}
	in := raw["input"]
	if len(in) == 0 || dotJSONNull(in) {
		return msgs, nil
	}
	var asString string
	if err := json.Unmarshal(in, &asString); err == nil {
		if strings.TrimSpace(asString) == "" {
			return nil, fmt.Errorf("input")
		}
		msgs = append(msgs, dotbridge.Message{Role: "user", Content: asString})
		return msgs, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(in, &items); err != nil {
		return nil, fmt.Errorf("input")
	}
	for _, item := range items {
		m, err := dotResponsesItem(item)
		if err != nil {
			return nil, err
		}
		if m.Role != "" {
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

func dotResponsesItem(item json.RawMessage) (dotbridge.Message, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(item, &obj); err != nil {
		return dotbridge.Message{}, fmt.Errorf("input item")
	}
	typ := stringField(obj, "type")
	switch typ {
	case "message":
		role := stringField(obj, "role")
		if role == "developer" {
			return dotbridge.Message{}, fmt.Errorf("developer")
		}
		if role != "system" && role != "user" && role != "assistant" {
			return dotbridge.Message{}, fmt.Errorf("role %s", role)
		}
		content, err := dotTextContent(obj["content"])
		if err != nil {
			return dotbridge.Message{}, err
		}
		return dotbridge.Message{Role: role, Content: content}, nil
	case "input_text":
		text := stringField(obj, "text")
		if text == "" {
			return dotbridge.Message{}, fmt.Errorf("input_text")
		}
		return dotbridge.Message{Role: "user", Content: text}, nil
	default:
		if typ == "" {
			return dotbridge.Message{}, fmt.Errorf("input item type")
		}
		return dotbridge.Message{}, fmt.Errorf("input item %s", typ)
	}
}

func dotChatMessages(raw json.RawMessage) ([]dotbridge.Message, error) {
	if len(raw) == 0 || dotJSONNull(raw) {
		return nil, nil
	}
	var msgs []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, fmt.Errorf("messages")
	}
	out := make([]dotbridge.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "developer" {
			return nil, fmt.Errorf("developer")
		}
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
			return nil, fmt.Errorf("role %s", m.Role)
		}
		content, err := dotTextContent(m.Content)
		if err != nil {
			return nil, err
		}
		out = append(out, dotbridge.Message{Role: m.Role, Content: content})
	}
	return out, nil
}

func dotTextContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || dotJSONNull(raw) {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", fmt.Errorf("content")
	}
	var b strings.Builder
	for _, p := range parts {
		typ := stringField(p, "type")
		switch typ {
		case "text", "input_text", "output_text":
			text := stringField(p, "text")
			if text == "" {
				text = stringField(p, "content")
			}
			b.WriteString(text)
		case "", "text/plain":
			if t := stringField(p, "text"); t != "" {
				b.WriteString(t)
			}
		default:
			return "", fmt.Errorf("content %s", typ)
		}
	}
	return b.String(), nil
}

func dotCheckSubmission(mode string, msgs []dotbridge.Message, task string) error {
	if len(msgs) > 32 {
		return fmt.Errorf("too many messages")
	}
	var n int
	for _, m := range msgs {
		if !utf8.ValidString(m.Content) {
			return fmt.Errorf("invalid utf8")
		}
		n += len(m.Content)
	}
	if n > 256*1024 {
		return fmt.Errorf("message content too large")
	}
	_, err := dotbridge.NewAttempt(dotbridge.Submission{
		RequestID: "req-dot-preflight",
		Mode:      mode,
		SessionID: "sess-dot-preflight",
		Messages:  msgs,
		Task:      task,
	})
	if err != nil {
		return fmt.Errorf("submission %v", err)
	}
	return nil
}

func dotSubmission(from provider.Protocol, body []byte, requestID, sessionID string) (dotbridge.Submission, error) {
	req, err := dotRequirementsOf(from, body)
	if err != nil {
		return dotbridge.Submission{}, err
	}
	if !req.ok() {
		return dotbridge.Submission{}, fmt.Errorf("unsupported for dot: %s", strings.Join(req.Unsupported, ", "))
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return dotbridge.Submission{}, err
	}
	var msgs []dotbridge.Message
	if from == provider.Chat {
		msgs, err = dotChatMessages(raw["messages"])
	} else {
		msgs, err = dotResponsesMessages(raw)
	}
	if err != nil {
		return dotbridge.Submission{}, err
	}
	return dotbridge.Submission{
		RequestID: requestID,
		Mode:      dotbridge.ModeAnswer,
		SessionID: sessionID,
		Messages:  msgs,
	}, nil
}

func dotCandidates(cs []candidate, pl planned, req dotRequirements) ([]candidate, planned) {
	if req.ok() {
		return cs, pl
	}
	var kept []candidate
	var order []Weighed
	for i, c := range cs {
		if c.p.IsDot() {
			continue
		}
		kept = append(kept, c)
		if i < len(pl.order) {
			order = append(order, pl.order[i])
		}
	}
	pl.order = order
	pl.left = dotFilterWeighed(pl.left, func(w Weighed) bool { return w.Provider == provider.DotID })
	pl.held = dotFilterCandidates(pl.held, func(c candidate) bool { return c.p.IsDot() })
	pl.capped = dotFilterCandidates(pl.capped, func(c candidate) bool { return c.p.IsDot() })
	pl.unlisted = dotFilterCandidates(pl.unlisted, func(c candidate) bool { return c.p.IsDot() })
	pl.unlistedW = dotFilterWeighed(pl.unlistedW, func(w Weighed) bool { return w.Provider == provider.DotID })
	return kept, pl
}

func dotFilterCandidates(in []candidate, drop func(candidate) bool) []candidate {
	var out []candidate
	for _, c := range in {
		if !drop(c) {
			out = append(out, c)
		}
	}
	return out
}

func dotFilterWeighed(in []Weighed, drop func(Weighed) bool) []Weighed {
	var out []Weighed
	for _, w := range in {
		if !drop(w) {
			out = append(out, w)
		}
	}
	return out
}

func hasJSON(raw map[string]json.RawMessage, key string) bool {
	v, ok := raw[key]
	return ok && len(v) > 0 && !dotJSONNull(v)
}

func dotJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func stringField(obj map[string]json.RawMessage, key string) string {
	v, ok := obj[key]
	if !ok || dotJSONNull(v) {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return ""
	}
	return s
}

func dotExplicitUnsupported(w http.ResponseWriter, from provider.Protocol, model string, req dotRequirements) {
	msg := fmt.Sprintf("model %q is not supported by DotControl (%s)", model, strings.Join(req.Unsupported, ", "))
	writeError(w, from, http.StatusBadRequest, msg)
}
