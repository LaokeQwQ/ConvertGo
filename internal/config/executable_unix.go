//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func validateExecutable(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("path must name a regular executable file")
	}
	return nil
}
