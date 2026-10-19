package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func dotCheckFile(f *os.File, st os.FileInfo, private bool) error {
	if dotHardLinked(f.Name()) {
		return fmt.Errorf("dot: input must not be a hard link")
	}
	if private {
		if err := dotCheckPrivate(f, st); err != nil {
			return err
		}
		return nil
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	mode := st.Mode().Perm()
	if mode&0077 != 0 {
		return fmt.Errorf("dot: input must not be world/group readable")
	}
	return nil
}

func dotCheckPrivate(f *os.File, st os.FileInfo) error {
	switch runtime.GOOS {
	case "windows":
		return dotCheckPrivateWindows(f.Name())
	default:
		if st.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("dot: pairing file must be private to the current user")
		}
		return nil
	}
}

func dotCheckPrivateWindows(path string) error {
	// Windows ACL checks are platform-specific; pairing files must still be
	// regular, non-linked files owned by the current user.
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("dot: invalid input path")
	}
	if strings.Contains(abs, "..") {
		return fmt.Errorf("dot: invalid input path")
	}
	return nil
}
