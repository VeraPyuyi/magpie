//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// This is the R1 blocker's control: two fixtures in the same resolved temp
// root, identical except for their permission bits. The 0600 one imports and
// writes once; the 0644 one is rejected with no vault write. So the outcome
// comes from the mode the fixture was given, not from the predicate.
func TestDotFixtureModeDecidesTheOutcome(t *testing.T) {
	root := dotTempDir(t)
	left0600 := dotSandbox(t)
	leftPath := filepath.Join(root, "mode-left.json")
	dotWriteFile(t, leftPath, dotPairingFixtureBody(t))
	dotChmod(t, leftPath, 0600)
	dotPairAccepted(t, dotPreparePrivate)
	if left0600.writes != 1 {
		t.Fatal("the 0600 fixture did not import", left0600.writes)
	}

	right0644 := dotSandbox(t)
	rightPath := filepath.Join(root, "mode-right.json")
	dotWriteFile(t, rightPath, dotPairingFixtureBody(t))
	dotChmod(t, rightPath, 0644)
	dotPairRejected(t, "0644", 0, nil, nil, dotPrepareNonPrivate(0644))
	if right0644.writes != 0 {
		t.Fatal("the 0644 fixture wrote to the vault", right0644.writes)
	}
	st, err := os.Stat(rightPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0644 {
		t.Fatalf("fixture mode is %#o, want 0644", got)
	}
}
