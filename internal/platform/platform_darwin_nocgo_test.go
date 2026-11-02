//go:build darwin && !cgo

package platform

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDarwinNoCGOReportsUnavailableNativeServices(t *testing.T) {
	fonts, err := ListFonts()
	if err == nil || !reflect.DeepEqual(fonts, []string{DefaultFontFamily()}) {
		t.Fatalf("fonts=%v err=%v", fonts, err)
	}
	release, err := InhibitSleep(context.Background())
	if release != nil || err == nil || !strings.Contains(err.Error(), "cgo-enabled") {
		t.Fatalf("release=%v err=%v", release != nil, err)
	}
}
