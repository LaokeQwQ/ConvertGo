package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
)

func DefaultFontFamily() string {
	switch runtime.GOOS {
	case "windows":
		return "Segoe UI"
	case "darwin":
		return "-apple-system"
	default:
		return "sans-serif"
	}
}

func environmentLocale() string {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func fontCatalog(families []string, cause error) ([]string, error) {
	if cause != nil {
		return []string{DefaultFontFamily()}, fmt.Errorf("local font catalog unavailable; using system UI font: %w", cause)
	}
	seen := make(map[string]bool, len(families))
	fonts := make([]string, 0, len(families))
	for _, family := range families {
		family = strings.TrimSpace(family)
		key := strings.ToLower(family)
		if family != "" && !strings.HasPrefix(family, "@") && !seen[key] {
			fonts = append(fonts, family)
			seen[key] = true
		}
	}
	if len(fonts) == 0 {
		return []string{DefaultFontFamily()}, errors.New("local font catalog contained no font families; using system UI font")
	}
	sort.Slice(fonts, func(i, j int) bool { return strings.ToLower(fonts[i]) < strings.ToLower(fonts[j]) })
	return fonts, nil
}

func sleepContextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("sleep inhibition requires a context")
	}
	return ctx.Err()
}

func releaseOnCancel(ctx context.Context, cleanup func()) func() {
	var once sync.Once
	done := make(chan struct{})
	release := func() { once.Do(func() { cleanup(); close(done) }) }
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				release()
			case <-done:
			}
		}()
	}
	return release
}
