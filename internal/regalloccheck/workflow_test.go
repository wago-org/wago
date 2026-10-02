package regalloccheck

import (
	"os"
	"strings"
	"testing"
)

func TestLocalCorrectnessWorkflowsEnableChecker(t *testing.T) {
	for _, path := range []string{
		"../../.just/test.just", "../../.just/spec.just",
		"../../scripts/verification.sh", "../../scripts/coverage.sh",
		"../../scripts/tests-card.sh", "../../scripts/spec-card.sh",
		"../../tests/scripts/regression-stress.sh",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") || !strings.Contains(line, "go test") {
				continue
			}
			// The ordinary root/CLI passes are intentional companions to checked runs.
			if path == "../../.just/test.just" && (line == "go test -count=1 ./..." || line == "go test -count=1 -tags wago_runtime ./cli/...") {
				continue
			}
			if !strings.Contains(line, "wago_regalloccheck") {
				t.Errorf("%s: missing checker tag: %s", path, line)
			}
		}
	}
}
