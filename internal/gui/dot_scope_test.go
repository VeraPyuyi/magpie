package gui

import (
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/dotbridge"
)

func TestDotGUIPairingGenerationScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	dotbridge.SetJournalPath(filepath.Join(t.TempDir(), "requests.json"))
	t.Cleanup(func() { dotbridge.SetJournalPath("") })
	cfg := dotbridge.Config{Enabled: true, Address: "127.0.0.1:3999", Credential: "same-reference", AnswerWaitSeconds: 120}
	if err := dotbridge.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	first, err := dotbridge.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	sub := dotbridge.Submission{RequestID: "old-request", Mode: "task", Task: "old pairing context"}
	if _, _, err := dotbridge.Reserve(sub, dotGUIScope(first)); err != nil {
		t.Fatal(err)
	}
	// The identity helper is authoritative: a pairing replacement at the same
	// address/reference must not expose the old pairing's GUI request journal.
	replacement := first
	replacement.PairingGeneration = "replacement-generation"
	if dotGUIScope(first) == dotGUIScope(replacement) {
		t.Fatal("pairing generations share GUI scope")
	}
	if dotGUIScope(first) != "gui:"+dotbridge.PairingIdentity(first) {
		t.Fatal("GUI does not use exported pairing identity")
	}
	items, err := dotbridge.Requests(dotGUIScope(replacement))
	if err != nil || len(items) != 0 {
		t.Fatalf("old pairing history leaked: %+v %v", items, err)
	}
	reread, err := dotbridge.LoadConfig()
	if err != nil || dotGUIScope(first) != dotGUIScope(reread) {
		t.Fatalf("loaded pairing scope not stable: %v", err)
	}
}
