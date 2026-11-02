//go:build darwin && !cgo

package platform

import (
	"context"
	"errors"
)

func SystemLocale() string { return environmentLocale() }

func ListFonts() ([]string, error) {
	return fontCatalog(nil, errors.New("macOS font enumeration requires a cgo-enabled build"))
}

func InhibitSleep(ctx context.Context) (func(), error) {
	if err := sleepContextError(ctx); err != nil {
		return nil, err
	}
	return nil, errors.New("sleep inhibition inactive: macOS power assertions require a cgo-enabled build")
}
