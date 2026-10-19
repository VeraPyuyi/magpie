//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func filepathJoinTmp(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "input.json")
}

func dotTestPrivate(t *testing.T, path string) {
	t.Helper()
	dotGrantOnlyCurrentUser(t, path, true)
}

// A Windows fixture that is deliberately not private: an unprotected DACL, so
// the inherited grants from the profile directory are the reason it fails.
func dotTestBroadAccess(t *testing.T, path string) {
	t.Helper()
	dotGrantOnlyCurrentUser(t, path, false)
}

func dotPreparePrivate(t *testing.T, path string) { dotTestPrivate(t, path) }

func dotPrepareBroadAccess(t *testing.T, path string) { dotTestBroadAccess(t, path) }

// dotApplySDDL writes a real security descriptor to a real temporary file. The
// protected flag decides whether inherited entries are blocked or allowed.
func dotApplySDDL(t *testing.T, path, sddl string, protected bool) {
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
	info := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.OWNER_SECURITY_INFORMATION)
	if protected {
		info |= windows.PROTECTED_DACL_SECURITY_INFORMATION
	} else {
		info |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, info, owner, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	// Keep the FILE_ATTRIBUTE_NORMAL mode deterministic; a read-only bit is not
	// what any of these cases are about, and chmod can fail on a fixture whose
	// DACL has just been replaced, which would be the fixture's own doing.
	_ = os.Chmod(path, 0600)
}

func dotGrantOnlyCurrentUser(t *testing.T, path string, protected bool) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	sddl := "O:" + sid + "D:(A;;FA;;;" + sid + ")"
	if protected {
		sddl = "O:" + sid + "D:P(A;;FA;;;" + sid + ")"
	}
	dotApplySDDL(t, path, sddl, protected)
}

// The input predicate itself, on real temporary files with real descriptors.
func TestDotInputWindowsAccess(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	for _, tc := range []struct {
		why       string
		sddl      string
		protected bool
		ok        bool
	}{
		{"protected-current-user", "O:" + sid + "D:P(A;;FA;;;" + sid + ")", true, true},
		{"everyone-readable", "O:" + sid + "D:P(A;;FA;;;" + sid + ")(A;;FR;;;" + everyone.String() + ")", true, false},
		{"unprotected-inherited", "O:" + sid + "D:(A;;FA;;;" + sid + ")", false, false},
		{"empty-dacl", "O:" + sid + "D:", true, false},
	} {
		t.Run(tc.why, func(t *testing.T) {
			p := filepathJoinTmp(t)
			if err := os.WriteFile(p, []byte(`{}`), 0600); err != nil {
				t.Fatal(err)
			}
			dotApplySDDL(t, p, tc.sddl, tc.protected)
			_, err := dotRead(p, true, 16384)
			if (err == nil) != tc.ok {
				t.Fatalf("accepted=%v err=%v", tc.ok, err)
			}
		})
	}
}

// Both DACL fixtures are real temporary files with real security descriptors;
// the success case is the one the accepted import depends on.
func TestDotPairInputWindowsAccess(t *testing.T) {
	t.Run("protected-current-user", func(t *testing.T) {
		dotPairAccepted(t, dotPreparePrivate)
	})
	t.Run("unprotected-inherited", func(t *testing.T) {
		dotPairRejected(t, "unprotected-inherited", 0, nil, nil, dotPrepareBroadAccess)
	})
	t.Run("everyone-readable", func(t *testing.T) {
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			t.Fatal(err)
		}
		everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
		if err != nil {
			t.Fatal(err)
		}
		sddl := "O:" + user.User.Sid.String() + "D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;" + everyone.String() + ")"
		grant := func(t *testing.T, path string) { dotApplySDDL(t, path, sddl, true) }
		dotPairRejected(t, "everyone-readable", 0, nil, nil, grant)
	})
}
