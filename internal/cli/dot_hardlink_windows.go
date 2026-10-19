//go:build windows

package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

func dotHardLinked(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var fi windows.ByHandleFileInformation
	return windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &fi) == nil && fi.NumberOfLinks > 1
}
