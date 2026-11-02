package platform

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var fontGDI = windows.NewLazySystemDLL("gdi32.dll")
var fontUser = windows.NewLazySystemDLL("user32.dll")
var localeKernel = windows.NewLazySystemDLL("kernel32.dll")

var fontEnumCallback = windows.NewCallback(func(font, metrics, fontType, parameter uintptr) uintptr {
	value := (*logFont)(unsafe.Pointer(font))
	families := (*[]string)(unsafe.Pointer(parameter))
	*families = append(*families, windows.UTF16ToString(value.FaceName[:]))
	return 1
})

type logFont struct {
	Height         int32
	Width          int32
	Escapement     int32
	Orientation    int32
	Weight         int32
	Italic         byte
	Underline      byte
	StrikeOut      byte
	CharSet        byte
	OutPrecision   byte
	ClipPrecision  byte
	Quality        byte
	PitchAndFamily byte
	FaceName       [32]uint16
}

func SystemLocale() string {
	proc := localeKernel.NewProc("GetUserDefaultLocaleName")
	if proc.Find() != nil {
		return environmentLocale()
	}
	var name [85]uint16
	result, _, _ := proc.Call(uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	if result == 0 {
		return environmentLocale()
	}
	return windows.UTF16ToString(name[:])
}

func ListFonts() ([]string, error) {
	getDC := fontUser.NewProc("GetDC")
	releaseDC := fontUser.NewProc("ReleaseDC")
	enumerate := fontGDI.NewProc("EnumFontFamiliesExW")
	for _, proc := range []*windows.LazyProc{getDC, releaseDC, enumerate} {
		if err := proc.Find(); err != nil {
			return fontCatalog(nil, err)
		}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dc, _, callErr := getDC.Call(0)
	if dc == 0 {
		return fontCatalog(nil, fmt.Errorf("GetDC failed: %v", callErr))
	}
	defer releaseDC.Call(0, dc)
	families := make([]string, 0, 128)
	query := logFont{CharSet: 1}
	enumerate.Call(dc, uintptr(unsafe.Pointer(&query)), fontEnumCallback, uintptr(unsafe.Pointer(&families)), 0)
	runtime.KeepAlive(&families)
	runtime.KeepAlive(&query)
	return fontCatalog(families, nil)
}
