package platform

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestLinuxLocaleEnvironmentPrecedence(t *testing.T) {
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LC_MESSAGES", "en_US.UTF-8")
	t.Setenv("LANG", "fr_FR.UTF-8")
	if got := SystemLocale(); got != "zh_CN.UTF-8" {
		t.Fatalf("LC_ALL lost: %q", got)
	}
	t.Setenv("LC_ALL", "")
	if got := SystemLocale(); got != "en_US.UTF-8" {
		t.Fatalf("LC_MESSAGES lost: %q", got)
	}
	t.Setenv("LC_MESSAGES", "")
	if got := SystemLocale(); got != "fr_FR.UTF-8" {
		t.Fatalf("LANG lost: %q", got)
	}
}

func TestLinuxMissingFontconfigReturnsSystemFontWithError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got, err := ListFonts()
	if err == nil || !reflect.DeepEqual(got, []string{DefaultFontFamily()}) {
		t.Fatalf("fonts=%v err=%v", got, err)
	}
}

func TestLinuxUnavailableLogindReportsInactive(t *testing.T) {
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path="+t.TempDir()+"/missing-bus")
	release, err := InhibitSleep(context.Background())
	if release != nil || err == nil || !strings.Contains(err.Error(), "sleep inhibition inactive") {
		t.Fatalf("release=%v err=%v", release != nil, err)
	}
}
