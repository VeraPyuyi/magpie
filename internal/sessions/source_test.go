package sessions

import (
	"encoding/json"
	"strings"
	"testing"
)

// A session read from an agent's own files is localhost's, wherever the
// page it reaches (the recent list, /api/sessions/manage) reads it; a
// WSL distro's sessions are read by this computer and keep their distro
// in WSL. Without the change these fixtures marshal no "source" at all.
func TestLocalSessionsIncludeSourceIdentity(t *testing.T) {
	manageSetup(t)
	recent := List(All)
	if len(recent) == 0 {
		t.Fatal("no recent sessions")
	}
	for _, s := range recent {
		if s.Source != "localhost" {
			t.Errorf("recent %s session %s source = %q, want localhost", s.Agent, s.ID, s.Source)
		}
	}
	for _, agent := range []string{"claude", "codex"} {
		list := ListAgent(agent)
		if len(list) == 0 {
			t.Fatalf("no %s sessions", agent)
		}
		for _, s := range list {
			b, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var out map[string]any
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatal(err)
			}
			if out["source"] != "localhost" {
				t.Errorf("%s session %s source = %v, want localhost", agent, s.ID, out["source"])
			}
		}
	}
}

// A session in a WSL distro is read by this computer through the distro,
// so it is localhost's session too; the distro stays in WSL.
func TestWSLSessionsIncludeSourceIdentity(t *testing.T) {
	home, _ := wslDistro(t)
	wslSettle()
	var n int
	for _, s := range List(All) {
		if s.WSL != "Ubuntu" || !strings.HasPrefix(s.Path, home) {
			continue
		}
		n++
		if s.Source != "localhost" {
			t.Errorf("%s session %s source = %q, want localhost", s.Agent, s.ID, s.Source)
		}
	}
	if n == 0 {
		t.Fatal("no WSL session from the distro")
	}
}

// A gateway projection is not read from an agent's file here, so nothing
// knows where it came from and the key stays out of its JSON.
func TestGatewaySessionsOmitSource(t *testing.T) {
	out := MergeExternal(nil, []ExternalSession{{Agent: "claude", ID: "ses-gw", Title: "asked", Models: []ExternalModel{{Model: "opus-x", Priced: true}}}})
	if len(out) != 1 {
		t.Fatalf("gateway sessions %+v, want one", out)
	}
	if out[0].Source != "" {
		t.Errorf("source = %q, want none", out[0].Source)
	}
	b, err := json.Marshal(out[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"source"`) {
		t.Errorf("a gateway row carries a source: %s", b)
	}
	// a native row beside it keeps localhost's, and wins the identity
	merged := MergeExternal([]Session{{Agent: "claude", ID: "ses-gw", Source: "localhost"}}, []ExternalSession{{Agent: "claude", ID: "ses-gw"}})
	if len(merged) != 1 || merged[0].Source != "localhost" || merged[0].Gateway {
		t.Errorf("merged %+v", merged)
	}
}
