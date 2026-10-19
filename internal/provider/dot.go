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
	return dotbridge.CachedReady(time.Now())
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

func dotModels() []catalog.Model {
	return []catalog.Model{{
		ID:       DotTextModel,
		Name:     "Dot text",
		Provider: DotID,
	}}
}

func dotProbeRequired(model string) string {
	if model != DotTextModel {
		return "dot bridge serves only dot/text"
	}
	if !dotMachineEnabled() {
		return "dot bridge is not ready; run magpie dot status and complete pairing/probe"
	}
	return ""
}
