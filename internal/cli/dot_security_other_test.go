//go:build !windows

package cli

import (
	"os"
	"testing"
)

func dotTestPrivate(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
}
