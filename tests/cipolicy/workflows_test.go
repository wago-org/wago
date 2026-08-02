package cipolicy

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var immutableAction = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[0-9a-f]{40}(?:\s+#\s+.+)?$`)

func TestWorkflowActionsUseImmutableCommits(t *testing.T) {
	paths, err := filepath.Glob(filepath.Clean("../../.github/workflows/*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yamlPaths, err := filepath.Glob(filepath.Clean("../../.github/workflows/*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, yamlPaths...)
	if len(paths) == 0 {
		t.Fatal("no workflows found")
	}
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		for lineNo := 1; scanner.Scan(); lineNo++ {
			line := strings.TrimSpace(scanner.Text())
			line = strings.TrimPrefix(line, "-")
			line = strings.TrimSpace(line)
			value, ok := strings.CutPrefix(line, "uses:")
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			if strings.HasPrefix(value, "./") || strings.HasPrefix(value, "docker://") {
				continue
			}
			if !immutableAction.MatchString(value) {
				t.Errorf("%s:%d action is not pinned to a full commit SHA: %s", filepath.Base(path), lineNo, value)
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
}
