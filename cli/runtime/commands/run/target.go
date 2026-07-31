package run

import (
	"os"
	"strings"
)

func LooksLikeTarget(value string) bool {
	if strings.HasSuffix(value, ".wasm") || strings.HasSuffix(value, ".wago") {
		return true
	}
	info, err := os.Stat(value)
	return err == nil && !info.IsDir()
}
