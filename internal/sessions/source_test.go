package sessions

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionIncludesSourceIdentity(t *testing.T) {
	s := Session{Agent: "codex", ID: "test-id", Source: "localhost"}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"source":"localhost"`) {
		t.Fatal("Source field missing in serialization")
	}
	s.Source = ""
	b, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"source"`) {
		t.Fatal("unknown source should be omitted")
	}
}

func TestListedDefaultsSourceIdentity(t *testing.T) {
	for _, source := range []string{"", "other-host"} {
		s := Session{Agent: "codex", ID: "test-id", Source: source, ReadOnly: true}
		listed(&s)
		want := source
		if want == "" {
			want = "localhost"
		}
		if s.Source != want {
			t.Errorf("listed source = %q, want %q", s.Source, want)
		}
	}
}

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
