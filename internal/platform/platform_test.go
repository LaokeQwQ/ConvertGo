package platform

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFontCatalogSortedAndDeduplicated(t *testing.T) {
	got, err := fontCatalog([]string{" Zebra ", "Alpha", "alpha", "", "@Vertical", "Zebra"}, nil)
	if err != nil || !reflect.DeepEqual(got, []string{"Alpha", "Zebra"}) {
		t.Fatalf("fonts=%v err=%v", got, err)
	}
}

func TestFontCatalogFailureIncludesDefaultAndError(t *testing.T) {
	cause := errors.New("catalog unavailable")
	fonts, err := fontCatalog(nil, cause)
	if !errors.Is(err, cause) || !reflect.DeepEqual(fonts, []string{DefaultFontFamily()}) {
		t.Fatalf("fonts=%v err=%v", fonts, err)
	}
	fonts, err = fontCatalog([]string{" ", "@Vertical"}, nil)
	if err == nil || !reflect.DeepEqual(fonts, []string{DefaultFontFamily()}) {
		t.Fatalf("empty fonts=%v err=%v", fonts, err)
	}
}

func TestReleaseRunsOnceAcrossConcurrentCallers(t *testing.T) {
	var calls atomic.Int32
	release := releaseOnCancel(context.Background(), func() { calls.Add(1) })
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() { defer workers.Done(); release() }()
	}
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("release called %d times", calls.Load())
	}
}
func TestCancellationReleasesInhibitor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	closed := make(chan struct{})
	release := releaseOnCancel(ctx, func() { close(closed) })
	cancel()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not release inhibitor")
	}
	release()
}

func TestInhibitSleepRejectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	release, err := InhibitSleep(ctx)
	if !errors.Is(err, context.Canceled) || release != nil {
		t.Fatalf("release=%v err=%v", release != nil, err)
	}
}

func TestInhibitSleepRejectsNilContext(t *testing.T) {
	release, err := InhibitSleep(nil)
	if err == nil || release != nil {
		t.Fatalf("release=%v err=%v", release != nil, err)
	}
}
