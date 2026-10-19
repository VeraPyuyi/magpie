//go:build !windows

package cli

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// This is the R1 blocker's control: a fixture that keeps the 0600 mode
// os.WriteFile just gave it takes the success path, so a case that expects
// rejection fails on its own assertion. The paired assertion below shows the
// same fixture with a real chmod to 0644 is rejected with no vault write.
func TestDotFixtureModeDecidesTheOutcome(t *testing.T) {
	left0600 := dotSandbox(t)
	leftPath := dotPairFixture(t, nil, nil, dotStatusPaired)
	var leftOut bytes.Buffer
	if err := dotTo(context.Background(), &leftOut, []string{"pair", leftPath}); err != nil {
		t.Fatalf("0600 fixture rejected: %v", err)
	}
	if left0600.writes != 1 {
		t.Fatal("the 0600 fixture did not import", left0600.writes)
	}

	right0644 := dotSandbox(t)
	rightPath := dotPairFixture(t, dotPrepareNonPrivate(0644), nil, dotStatusPaired)
	var rightOut bytes.Buffer
	if err := dotTo(context.Background(), &rightOut, []string{"pair", rightPath}); err == nil {
		t.Fatal("a 0644 fixture was accepted")
	}
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
