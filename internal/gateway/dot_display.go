package gateway

import (
	"strings"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// DotSkipped is one configured Dot member removed before execution. It is
// observational only: it never makes a candidate eligible or sends work.
type DotSkipped struct {
	Model      string    `json:"model"`
	Mode       string    `json:"mode,omitempty"`
	Reason     string    `json:"reason"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
}

func dotDisplaySkipped(from provider.Protocol, body []byte, cs []candidate, pl planned) []DotSkipped {
	seen := map[string]bool{}
	var out []DotSkipped
	add := func(model string) {
		if model == "" || seen[model] {
			return
		}
		reason := dotSkipReason(from, body, model)
		if reason == "" {
			return
		}
		seen[model] = true
		out = append(out, DotSkipped{Model: model, Mode: dotModeOfModel(model), Reason: dotSkipReasonLabel(reason)})
	}
	for _, c := range cs {
		if c.p.IsDot() {
			add(c.model)
		}
	}
	for _, w := range append(append([]Weighed{}, pl.left...), pl.unlistedW...) {
		if w.Provider == provider.DotID && !w.Held {
			add(w.Model)
		}
	}
	for _, list := range [][]candidate{pl.held, pl.capped, pl.unlisted} {
		for _, c := range list {
			if c.p.IsDot() {
				add(c.model)
			}
		}
	}
	return out
}

func dotSkipReason(from provider.Protocol, body []byte, model string) string {
	req, err := dotRequirementsFor(from, body, model)
	if err != nil {
		return err.Error()
	}
	if !req.ok() {
		return strings.Join(req.Unsupported, ", ")
	}
	return ""
}

func dotSkipReasonLabel(reason string) string {
	// Return fixed categories only: unsupported field names and parser errors
	// may be controlled by the caller and never belong in durable trace data.
	switch {
	case strings.Contains(reason, "tools"), strings.Contains(reason, "tool_choice"), strings.Contains(reason, "function_call"):
		return "caller_tools"
	case strings.Contains(reason, "local environment"):
		return "local_environment"
	case strings.Contains(reason, "task prompt"):
		return "task_prompt_required"
	case strings.Contains(reason, "unavailable"):
		return "mode_unavailable"
	default:
		return "unsupported_request"
	}
}
