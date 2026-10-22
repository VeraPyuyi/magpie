package dotbridge

import (
	"strings"
	"testing"
)

func TestArtifactProducerHostPolicy(t *testing.T) {
	for _, u := range []string{"https://localhost./a", "https://x.localhost./a", "https://intranet/a", "https://2130706433/a", "https://127.1/a", "https://0x7f000001/a", "https://0177.0.0.1/a", "https://224.0.0.1/a", "https://100.64.0.1/a", "https://x.local/a", "https://example.com:65536/a", "https://example.com:0/a", "https://user:pass@example.com/a", "https://example.com/" + string([]byte{255})} {
		if ValidateResult(&Result{Artifacts: []Artifact{{URL: u}}}) == nil {
			t.Errorf("accepted %q", u)
		}
	}
	for _, u := range []string{"https://example.com/a", "https://example.com:443/a", "https://8.8.8.8/a"} {
		if ValidateResult(&Result{Artifacts: []Artifact{{URL: u}}}) != nil {
			t.Errorf("rejected %s", u)
		}
	}
	a := Artifact{URL: "https://example.com/a", Label: strings.Repeat("x", 256)}
	r := &Result{Text: strings.Repeat("t", 256*1024-len(a.URL)-len(a.Label)), Artifacts: []Artifact{a}}
	if ValidateResult(r) != nil {
		t.Error("exact combined budget rejected")
	}
	r.Text += "t"
	if ValidateResult(r) == nil {
		t.Error("over combined budget accepted")
	}
}
