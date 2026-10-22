package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// dotTempDir is a trusted temporary directory with every symlink component
// resolved. macOS puts t.TempDir() under /var, which is a symlink to
// /private/var, and a TMPDIR an admin arranged through a symlink does the same
// on Linux. The production traversal refuses every symlink component starting
// at /, so a fixture built from an unresolved temp path would be rejected for
// the wrong reason. This is the fixture root only: production still refuses any
// link the cases below put in the path on purpose.
func dotTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// dotTempFile joins a fixture filename onto the resolved temp root.
func dotTempFile(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(dotTempDir(t), name)
}

// dotChmod applies mode and reads it back, so a fixture cannot silently keep
// the 0600 os.WriteFile gave it.
func dotChmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != mode {
		t.Fatalf("mode is %#o, want %#o", got, mode)
	}
}

func dotWriteFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
}
