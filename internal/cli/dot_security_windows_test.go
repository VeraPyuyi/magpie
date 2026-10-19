package cli

import (
	"bytes"
	"context"
	"golang.org/x/sys/windows"
	"os"
	"strings"
	"testing"
)

func dotTestPrivate(t *testing.T, path string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	dotTestACL(t, path, "O:"+user.User.Sid.String()+"D:P(A;;FA;;;"+user.User.Sid.String()+")")
}
func dotTestACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION | windows.OWNER_SECURITY_INFORMATION)
	if !strings.Contains(sddl, "D:P") {
		flags = windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION | windows.OWNER_SECURITY_INFORMATION
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, flags, owner, nil, dacl, nil)
	if err != nil {
		t.Fatal(err)
	}
}
func TestDotReviewWindowsPrivacy(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	for _, tc := range []struct {
		name, sddl string
		ok         bool
	}{
		{"private", "O:" + sid + "D:P(A;;FA;;;" + sid + ")", true},
		{"everyone", "O:" + sid + "D:P(A;;FA;;;" + sid + ")(A;;FR;;;WD)", false},
		{"inherited-policy", "O:" + sid + "D:(A;;FA;;;" + sid + ")", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := t.TempDir() + "\\input.json"
			if err := os.WriteFile(p, []byte(`{}`), 0600); err != nil {
				t.Fatal(err)
			}
			dotTestACL(t, p, tc.sddl)
			_, err := dotRead(p, true, 16384)
			if (err == nil) != tc.ok {
				t.Fatalf("private=%v err=%v", tc.ok, err)
			}
		})
	}
}

func TestDotReviewHandleBoundaries(t *testing.T) {
	p := t.TempDir() + "\\input.json"
	os.WriteFile(p, []byte(`{}`), 0600)
	dotTestPrivate(t, p)
	f, closeInput, err := dotOpenInput(p)
	if err != nil {
		t.Fatal(err)
	}
	defer closeInput()
	if err := os.Rename(p, p+".moved"); err == nil {
		t.Fatal("opened leaf was replaceable")
	}
	if err := dotCheckFile(f, nil, true); err != nil {
		t.Fatal(err)
	}
	// Query failures must not turn into a negative hard-link/privacy result.
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
	if _, err := dotRead(p, true, 16384); err == nil {
		t.Error("hard-linked input accepted")
	}
}

func TestDotReviewAncestorSubstitution(t *testing.T) {
	dir := t.TempDir()
	p := dir + "\\input.json"
	os.WriteFile(p, []byte(`{}`), 0600)
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
	if b, err := dotRead(p, true, 16384); err != nil || string(b) != `{}` {
		t.Fatalf("substitution changed input: %s %v", b, err)
	}
	if !attempted {
		t.Fatal("substitution point not reached")
	}
}

func TestDotReviewWrongOwnerFailsBeforeHTTP(t *testing.T) {
	store := dotSandbox(t)
	p := t.TempDir() + "\\pair.json"
	os.WriteFile(p, []byte(`{"version":1,"client_id":"operator","queue_id":"operator","token":"sentinel","listen":"127.0.0.1:1","created_at":"2026-10-19T00:00:00Z"}`), 0600)
	dotTestPrivate(t, p)
	original := dotSecurityInfo
	defer func() { dotSecurityInfo = original }()
	// Exercise the owner predicate without requiring SeRestorePrivilege to assign
	// an arbitrary owner's SID to a real file. ACL acceptance uses real files above.
	dotSecurityInfo = func(h windows.Handle, typ windows.SE_OBJECT_TYPE, flags windows.SECURITY_INFORMATION) (*windows.SECURITY_DESCRIPTOR, error) {
		sd, err := original(h, typ, flags)
		if err != nil {
			return nil, err
		}
		foreign, err := windows.CreateWellKnownSid(windows.WinWorldSid)
		if err != nil {
			return nil, err
		}
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			return nil, err
		}
		_ = sd // The real handle query above must succeed before substituting owner.
		return windows.SecurityDescriptorFromString("O:" + foreign.String() + "D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	}
	var out bytes.Buffer
	if err := dotTo(context.Background(), &out, []string{"pair", p}); err == nil || store.writes != 0 || out.Len() != 0 {
		t.Fatal("foreign owner accepted", err, store.writes)
	}
}
