//go:build linux && amd64 && tinygo

package amd64

import (
	"os"
	"strings"
)

// Match the runtime's Linux/TinyGo strategy: require every reported processor
// to support the test's instruction tier. No x/sys assembly is linked.
func workerResetModernCPU() bool {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}
	seen := false
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "flags" {
			continue
		}
		flags := " " + strings.Join(strings.Fields(value), " ") + " "
		for _, required := range []string{"ssse3", "sse4_1", "sse4_2", "avx"} {
			if !strings.Contains(flags, " "+required+" ") {
				return false
			}
		}
		seen = true
	}
	return seen
}
