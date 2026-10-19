package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dotOpenInput traverses relative to held directory handles. Renaming an
// ancestor cannot redirect the next open. Reparse traversal is forbidden.
// The leaf denies write/delete sharing; checks and reads use that same handle.
// This does not isolate against hostile code with the current user's rights.
var dotOpenComponentHook func(string, int)

func dotOpenInput(path string) (*os.File, func(), error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	volume := filepath.VolumeName(abs)
	if len(volume) != 2 || volume[1] != ':' {
		return nil, nil, fmt.Errorf("dot: only local drive paths supported")
	}
	parts := strings.Split(strings.TrimPrefix(abs[len(volume):], `\`), `\`)
	current := volume + `\`
	var handles []windows.Handle
	closeAll := func() {
		for i := len(handles) - 1; i >= 0; i-- {
			windows.CloseHandle(handles[i])
		}
	}
	fail := func() (*os.File, func(), error) {
		closeAll()
		return nil, nil, fmt.Errorf("dot: cannot securely open input; links and reparse points are forbidden")
	}
	for i := -1; i < len(parts); i++ {
		if dotOpenComponentHook != nil {
			dotOpenComponentHook(current, i)
		}
		if i >= 0 {
			current = filepath.Join(current, parts[i])
		}
		access := uint32(windows.FILE_READ_ATTRIBUTES)
		if i == len(parts)-1 {
			access = windows.GENERIC_READ | windows.READ_CONTROL
		}
		name, e := windows.UTF16PtrFromString(current)
		if e != nil {
			return fail()
		}
		share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE)
		if i == len(parts)-1 {
			share = windows.FILE_SHARE_READ
		}
		var h windows.Handle
		if i < 0 {
			h, e = windows.CreateFile(name, access, share, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		} else {
			component, ce := windows.NewNTUnicodeString(parts[i])
			if ce != nil {
				return fail()
			}
			oa := windows.OBJECT_ATTRIBUTES{Length: uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})), RootDirectory: handles[len(handles)-1], ObjectName: component, Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE}
			var iosb windows.IO_STATUS_BLOCK
			options := uint32(windows.FILE_OPEN_REPARSE_POINT | windows.FILE_SYNCHRONOUS_IO_NONALERT)
			if i < len(parts)-1 {
				options |= windows.FILE_DIRECTORY_FILE
			} else {
				options |= windows.FILE_NON_DIRECTORY_FILE
			}
			e = windows.NtCreateFile(&h, access|windows.SYNCHRONIZE, &oa, &iosb, nil, 0, share, windows.FILE_OPEN, options, 0, 0)
		}
		if e != nil {
			return fail()
		}
		handles = append(handles, h)
		var info windows.ByHandleFileInformation
		if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fail()
		}
		if i < len(parts)-1 && info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return fail()
		}
	}
	leaf := handles[len(handles)-1]
	handles = handles[:len(handles)-1]
	f := os.NewFile(uintptr(leaf), abs)
	return f, func() { f.Close(); closeAll() }, nil
}

// Kept as a variable for deterministic failure injection in CLI tests.
var dotSecurityInfo = windows.GetSecurityInfo

func dotCheckFile(f *os.File, _ os.FileInfo, _ bool) error {
	bad := fmt.Errorf("dot: input must be a private current-user file with a protected DACL and one link")
	h := windows.Handle(f.Fd())
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &info) != nil || info.NumberOfLinks != 1 || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return bad
	}
	sd, err := dotSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return bad
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return bad
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return bad
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return bad
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return bad
	}
	// Accept only explicit current-user allow ACEs. Unknown/object/callback ACEs,
	// inherited ACEs and every grant to another SID fail closed.
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERITED_ACE != 0 {
			return bad
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.Equals(user.User.Sid) {
			return bad
		}
	}
	return nil
}
