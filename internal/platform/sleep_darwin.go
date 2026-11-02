//go:build darwin && cgo

package platform

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <CoreFoundation/CoreFoundation.h>

static IOReturn convertgo_create_sleep_assertion(IOPMAssertionID *identifier) {
	return IOPMAssertionCreateWithName(kIOPMAssertionTypePreventUserIdleSystemSleep,
		kIOPMAssertionLevelOn, CFSTR("ConvertGo is encoding media"), identifier);
}
*/
import "C"

import (
	"context"
	"fmt"
)

func InhibitSleep(ctx context.Context) (func(), error) {
	if err := sleepContextError(ctx); err != nil {
		return nil, err
	}
	var identifier C.IOPMAssertionID
	if result := C.convertgo_create_sleep_assertion(&identifier); result != 0 {
		return nil, fmt.Errorf("sleep inhibition inactive: macOS power assertion failed (IOKit status 0x%x)", uint32(result))
	}
	release := releaseOnCancel(ctx, func() { C.IOPMAssertionRelease(identifier) })
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
