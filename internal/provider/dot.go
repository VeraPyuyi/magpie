package provider

import (
	"time"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/dotbridge"
)

const (
	DotID        = "dot"
	DotName      = "DotControl"
	DotTextModel = "text"
	DotTaskModel = "task"
)

// IsDot reports whether p is the built-in DotControl bridge provider.
func (p Provider) IsDot() bool { return p.dot }

// DotProvider returns the synthetic Dot provider when machine-local bridge
// configuration is enabled and verified ready.
func DotProvider() (Provider, bool) {
	if !dotMachineEnabled() {
		return Provider{}, false
	}
	return dotBuiltin(), true
}

// DotConfigurationOption is a display/configuration entry, never execution
// eligibility. A stored endpoint owning this ID retains its ordinary semantics.
func DotConfigurationOption() (Provider, bool) {
	if dotStoredCollision() {
		return Provider{}, false
	}
	return dotBuiltin(), true
}

func dotBuiltin() Provider {
	return Provider{
		ID:   DotID,
		Name: DotName,
		Icon: "dot",
		// Chat/Responses are listed so routing can speak either protocol;
		// the gateway Dot branch handles them without an HTTP base URL.
		Chat:      "dot://bridge",
		Responses: "dot://bridge",
		dot:       true,
	}
}

func dotMachineEnabled() bool {
	cfg, err := dotbridge.LoadConfig()
	if err != nil || !cfg.Enabled {
		return false
	}
	if _, err := dotbridge.PairedConnection(); err != nil {
		return false
	}
	now := time.Now()
	return dotbridge.CachedReady(now, dotbridge.CapabilityText) || dotbridge.CachedReady(now, dotbridge.CapabilityTasks)
}

func dotStoredCollision() bool {
	f := load()
	for _, p := range f.Providers {
		if p.ID == DotID && hasEndpoint(normalize(p)) {
			return true
		}
	}
	return false
}

// CallerToolsSupported requires every executable group member to accept the
// caller's tool loop; a group's first provider is not its capability contract.
func CallerToolsSupported(e Entry) bool {
	if e.Group == "" {
		return !e.Provider.IsDot()
	}
	_, members, ok := FindGroup(e.ID)
	if !ok || len(members) == 0 {
		return false
	}
	for _, m := range members {
		if m.Provider.IsDot() {
			return false
		}
	}
	return true
}

func dotModels() []catalog.Model {
	var models []catalog.Model
	for _, m := range []catalog.Model{{ID: DotTextModel, Name: "Dot text", Provider: DotID}, {ID: DotTaskModel, Name: "Dot task", Provider: DotID}} {
		if dotProbeRequired(m.ID) == "" {
			models = append(models, m)
		}
	}
	return models
}

func dotProbeRequired(model string) string {
	cap := dotbridge.CapabilityText
	if model == DotTaskModel {
		cap = dotbridge.CapabilityTasks
	} else if model != DotTextModel {
		return "dot bridge serves only dot/text and dot/task"
	}
	cfg, err := dotbridge.LoadConfig()
	if err != nil || !cfg.Enabled {
		return "dot bridge is not ready; run magpie dot status and complete pairing/probe"
	}
	if _, err := dotbridge.PairedConnection(); err != nil {
		return "dot bridge is not ready; run magpie dot status and complete pairing/probe"
	}
	if !dotbridge.CachedReady(time.Now(), cap) {
		return "dot bridge is not ready for " + model + "; run magpie dot status and complete pairing/probe"
	}
	return ""
}
