//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// Privacy on Unix is the mode bits: the fixture's own chmod result and the
// resulting mode are both asserted, so a case cannot silently stay at the
// 0600 that os.WriteFile just created and take the success path.
func dotTestPrivate(t *testing.T, path string) {
	t.Helper()
	dotChmod(t, path, 0600)
}

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

func dotRename(t *testing.T, from, to string) {
	t.Helper()
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(to) })
}

func dotSymlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		t.Fatal(err)
	}
}

// The private mode is the one the valid import depends on; every other mode
// here must be rejected before any vault, config or HTTP side effect.
func TestDotPairInputUnixModes(t *testing.T) {
	for _, mode := range []os.FileMode{0600, 0640, 0644, 0666} {
		t.Run(mode.String(), func(t *testing.T) {
			if mode == 0600 {
				dotPairAccepted(t, dotPreparePrivate)
				return
			}
			dotPairRejected(t, "mode "+mode.String(), 0, nil, nil, dotPrepareNonPrivate(mode))
		})
	}
}

func dotPrepareNonPrivate(mode os.FileMode) prepare {
	return func(t *testing.T, p string) { dotChmod(t, p, mode) }
}

func dotPreparePrivate(t *testing.T, path string) { dotTestPrivate(t, path) }

func TestDotPairInputUnixLinks(t *testing.T) {
	dotPairRejected(t, "symlink-leaf", 0, nil, nil, func(t *testing.T, p string) {
		dotRename(t, p, p+".real")
		dotSymlink(t, p+".real", p)
	})
	dotPairRejected(t, "hardlink-leaf", 0, nil, nil, func(t *testing.T, p string) {
		if err := os.Link(p, p+".link"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(p + ".link") })
	})
	dotPairRejected(t, "symlink-ancestor", 0, nil, nil, func(t *testing.T, p string) {
		dir := filepath.Dir(p)
		dotRename(t, dir, dir+".real")
		dotSymlink(t, dir+".real", dir)
	})
}

// The open traversal itself refuses links: it never follows an ancestor
// symlink to reach the file, so no file mode is consulted.
func TestDotOpenInputUnixRefusesLinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(real, "pair.json")
	if err := os.WriteFile(leaf, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, closeInput, err := dotOpenInput(leaf); err != nil {
		t.Fatalf("regular private file: %v", err)
	} else {
		closeInput()
	}
	linked := filepath.Join(dir, "linked")
	dotSymlink(t, real, linked)
	if _, closeInput, err := dotOpenInput(filepath.Join(linked, "pair.json")); err == nil {
		closeInput()
		t.Fatal("traversal through a symlinked ancestor was accepted")
	}
	leafLink := leaf + ".link"
	if err := os.Link(leaf, leafLink); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(leafLink) })
	if _, closeInput, err := dotOpenInput(leaf); err == nil {
		closeInput()
		t.Fatal("hard-linked leaf was accepted")
	}
}

// Ownership is checked against the effective user on the open descriptor.
func TestDotCheckFileUnixOwnership(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pair.json")
	if err := os.WriteFile(p, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	f, closeInput, err := dotOpenInput(p)
	if err != nil {
		t.Fatal(err)
	}
	defer closeInput()
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		t.Fatal(err)
	}
	if st.Uid != uint32(os.Geteuid()) {
		t.Fatalf("fixture owner %d is not the effective user %d", st.Uid, os.Geteuid())
	}
	if st.Nlink != 1 {
		t.Fatalf("fixture has %d links", st.Nlink)
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG {
		t.Fatalf("fixture is not a regular file")
	}
}
