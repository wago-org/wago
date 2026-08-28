//go:build linux

package compiler

import (
	"os"
	"strings"
)

func hostCPUBrand() string {
	contents, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	return cpuInfoBrand(string(contents))
}

func cpuInfoBrand(contents string) string {
	for _, key := range []string{"model name", "hardware", "processor"} {
		for _, line := range strings.Split(contents, "\n") {
			name, value, ok := strings.Cut(line, ":")
			if ok && strings.EqualFold(strings.TrimSpace(name), key) && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}
