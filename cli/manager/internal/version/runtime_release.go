package version

import (
	"os"
	"os/exec"
	"strings"
)

var runtimeVersionOutput = func(path string) ([]byte, error) {
	return exec.Command(path, "--version").Output()
}

func RuntimeRelease(path, fallback string) string {
	if current, err := os.Executable(); err == nil {
		currentInfo, currentErr := os.Stat(current)
		pathInfo, pathErr := os.Stat(path)
		if currentErr == nil && pathErr == nil && os.SameFile(currentInfo, pathInfo) {
			return fallback
		}
	}
	output, err := runtimeVersionOutput(path)
	if err != nil {
		return fallback
	}
	return ReleaseFromOutput(output, fallback)
}

func ReleaseFromOutput(output []byte, fallback string) string {
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "release") {
			return fields[1]
		}
	}
	if len(lines) != 0 {
		fields := strings.Fields(lines[0])
		if len(fields) >= 2 && strings.EqualFold(fields[0], "wago") {
			return fields[1]
		}
	}
	return fallback
}
