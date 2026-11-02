package platform

import (
	"context"
	"fmt"
	"runtime"
)

const executionContinuous = 0x80000000
const executionSystemRequired = 0x00000001

func InhibitSleep(ctx context.Context) (func(), error) {
	if err := sleepContextError(ctx); err != nil {
		return nil, err
	}
	setState := localeKernel.NewProc("SetThreadExecutionState")
	if err := setState.Find(); err != nil {
		return nil, fmt.Errorf("sleep inhibition inactive: execution-state API unavailable: %w", err)
	}
	started := make(chan error, 1)
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(finished)
		if err := ctx.Err(); err != nil {
			started <- err
			return
		}
		result, _, callErr := setState.Call(executionContinuous | executionSystemRequired)
		if result == 0 {
			started <- fmt.Errorf("sleep inhibition inactive: SetThreadExecutionState failed: %v", callErr)
			return
		}
		started <- nil
		select {
		case <-ctx.Done():
		case <-stop:
		}
		setState.Call(executionContinuous)
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	release := releaseOnCancel(ctx, func() { close(stop); <-finished })
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
