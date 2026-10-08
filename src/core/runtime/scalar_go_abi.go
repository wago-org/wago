//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package runtime

import (
	"strconv"
	"strings"
)

// Unknown internal Go ABIs retain the ordinary host loop.
func scalarGoABI(version string) bool {
	if !strings.HasPrefix(version, "go1.") {
		return false
	}
	minor := strings.SplitN(strings.TrimPrefix(version, "go1."), ".", 2)[0]
	value, err := strconv.Atoi(minor)
	return err == nil && value >= 22 && value <= 27
}
