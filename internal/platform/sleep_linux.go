package platform

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

func InhibitSleep(ctx context.Context) (func(), error) {
	if err := sleepContextError(ctx); err != nil {
		return nil, err
	}
	conn, err := dbus.ConnectSystemBus(dbus.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("sleep inhibition inactive: connect to system logind bus: %w", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var descriptor dbus.UnixFD
	err = conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").CallWithContext(callCtx, "org.freedesktop.login1.Manager.Inhibit", 0, "sleep", "ConvertGo", "Encoding media", "block").Store(&descriptor)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("sleep inhibition inactive: logind sleep inhibitor denied or unavailable: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), "ConvertGo-logind-inhibitor")
	if file == nil {
		conn.Close()
		return nil, fmt.Errorf("sleep inhibition inactive: logind returned an invalid inhibitor descriptor")
	}
	release := releaseOnCancel(ctx, func() { file.Close(); conn.Close() })
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
