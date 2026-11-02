package platform

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func SystemLocale() string { return environmentLocale() }

func ListFonts() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "fc-list", "--format=%{family[0]}\n").Output()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return fontCatalog(strings.Split(string(data), "\n"), err)
}
