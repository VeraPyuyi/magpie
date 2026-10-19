//go:build !windows

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func dotOpenInput(path string) (*os.File, func(), error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(abs, "/"), "/") {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		unix.Close(fd)
		if e != nil {
			return nil, nil, fmt.Errorf("dot: cannot securely open input without links")
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), abs)
	return f, func() { f.Close() }, nil
}

func dotCheckFile(f *os.File, _ os.FileInfo, _ bool) error {
	var st unix.Stat_t
	if unix.Fstat(int(f.Fd()), &st) != nil || st.Nlink != 1 || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("dot: input must be a private current-user regular file with one link")
	}
	return nil
}
