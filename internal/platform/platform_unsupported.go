//go:build !darwin && !windows && !linux

package platform

import (
	"context"
	"fmt"
	"runtime"
)

func SystemLocale() string { return environmentLocale() }

func ListFonts() ([]string, error) {
	return fontCatalog(nil, fmt.Errorf("font enumeration is unsupported on %s", runtime.GOOS))
}

func InhibitSleep(ctx context.Context) (func(), error) {
	if err := sleepContextError(ctx); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("sleep inhibition inactive: unsupported platform %s", runtime.GOOS)
}
