package config

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getBinaryType = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetBinaryTypeW")

func validateExecutable(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path must name a regular executable file")
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := getBinaryType.Find(); err != nil {
		return err
	}
	var kind uint32
	result, _, callErr := getBinaryType.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&kind)))
	if result == 0 {
		return fmt.Errorf("file is not a Windows executable: %v", callErr)
	}
	return nil
}
