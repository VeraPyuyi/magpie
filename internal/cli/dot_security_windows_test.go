package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// Real Windows handles and real temporary files: the opened file, its security
// descriptor and its link count, not a rewritten copy of the predicate.
func TestDotOpenInputHandleBoundaries(t *testing.T) {
	p := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(p, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	dotTestPrivate(t, p)
	f, closeInput, err := dotOpenInput(p)
	if err != nil {
		t.Fatal(err)
	}
	defer closeInput()
	if err := dotCheckFile(f, nil, true); err != nil {
		t.Fatal(err)
	}
	// The opened leaf refuses a rename: another name for it is impossible.
	if err := os.Rename(p, p+".moved"); err == nil {
		t.Fatal("opened leaf was replaceable")
	}
	// A security query that fails must not read as "not hard linked, is private".
	original := dotSecurityInfo
	dotSecurityInfo = func(windows.Handle, windows.SE_OBJECT_TYPE, windows.SECURITY_INFORMATION) (*windows.SECURITY_DESCRIPTOR, error) {
		return nil, os.ErrPermission
	}
	if err := dotCheckFile(f, nil, true); err == nil {
		t.Error("security query failure accepted")
	}
	dotSecurityInfo = original
	closeInput()
	if err := dotCheckFile(f, nil, true); err == nil {
		t.Error("closed handle accepted")
	}
	link := p + ".link"
	if err := os.Link(p, link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(link) })
	if _, err := dotRead(p, true, 16384); err == nil {
		t.Error("hard-linked input accepted")
	}
}

// Ancestor replacement cannot redirect the read: the open holds each parent.
// Windows denies the rename outright, so the fixture asserts the bytes are
// unchanged rather than asserting the rename succeeded.
func TestDotOpenInputAncestorSubstitution(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "input.json")
	if err := os.WriteFile(p, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	dotTestPrivate(t, p)
	attempted := false
	dotOpenComponentHook = func(current string, _ int) {
		if current == dir && !attempted {
			attempted = true
			if err := os.Rename(dir, dir+".moved"); err == nil {
				os.Mkdir(dir, 0700)
				os.WriteFile(p, []byte(`{"replaced":true}`), 0600)
				t.Cleanup(func() { os.RemoveAll(dir + ".moved") })
			}
		}
	}
	defer func() { dotOpenComponentHook = nil }()
	b, err := dotRead(p, true, 16384)
	if err != nil || string(b) != `{}` {
		t.Fatalf("substitution changed input: %s %v", b, err)
	}
	if !attempted {
		t.Fatal("substitution point not reached")
	}
}

// The wrong-owner branch is reached by substituting the descriptor after a
// real handle query, since assigning a foreign owner to a real file needs
// privileges this test does not have.
func TestDotPairForeignOwnerRejected(t *testing.T) {
	store := dotSandbox(t)
	foreign, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	original := dotSecurityInfo
	defer func() { dotSecurityInfo = original }()
	dotSecurityInfo = func(h windows.Handle, typ windows.SE_OBJECT_TYPE, flags windows.SECURITY_INFORMATION) (*windows.SECURITY_DESCRIPTOR, error) {
		sd, err := original(h, typ, flags)
		if err != nil {
			return nil, err
		}
		_ = sd // the real handle query must succeed before the owner is substituted
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			return nil, err
		}
		return windows.SecurityDescriptorFromString("O:" + foreign.String() + "D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	}
	path := dotPairFixture(t, dotPreparePrivate, nil, dotStatusPaired)
	var out bytes.Buffer
	if err := dotTo(context.Background(), &out, []string{"pair", path}); err == nil || store.writes != 0 || out.Len() != 0 {
		t.Fatal("foreign owner accepted", err, store.writes)
	}
}
