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

func TestRollingReleaseWorkflowsUseImmutableTagsAndTargets(t *testing.T) {
	for _, path := range []string{"../../.github/workflows/canary.yml", "../../.github/workflows/nightly.yml"} {
		workflow, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatal(err)
		}
		contents := string(workflow)
		for _, required := range []string{
			`target=$(gh api "repos/${{ github.repository }}/releases/tags/`,
			`has an invalid target commit`,
			`if [ "$target" != "${{ needs.`,
		} {
			if !strings.Contains(contents, required) {
				t.Errorf("%s is missing immutable existing-release validation %q", filepath.Base(path), required)
			}
		}
	}
	nightly, err := os.ReadFile(filepath.Clean("../../.github/workflows/nightly.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(nightly)
	if !strings.Contains(contents, `echo "tag=nightly-$(date -u +%Y%m%d)-$sha"`) {
		t.Fatal("nightly release tag must contain the full immutable commit SHA")
	}
	if strings.Contains(contents, `echo "tag=nightly-$(date -u +%Y%m%d)-${sha::7}"`) {
		t.Fatal("nightly release tag still uses an abbreviated commit SHA")
	}
}

func TestWindowsWABTInstallIsPinnedAndVerified(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(workflow)
	if strings.Contains(contents, "choco install wabt") {
		t.Fatal("Windows CI must not use the nonexistent Chocolatey wabt package")
	}
	if !strings.Contains(contents, "./tests/scripts/install-wabt-windows.ps1") {
		t.Fatal("Windows CI must use the pinned WABT installer")
	}

	installer, err := os.ReadFile(filepath.Clean("../scripts/install-wabt-windows.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	install := string(installer)
	for _, required := range []string{
		`[string]$Version = "1.0.41"`,
		`[string]$SHA256 = "37285ec7244384ffd382841f93fd23335aae846c92016a132d765c60f27a2f31"`,
		"https://github.com/WebAssembly/wabt/releases/download/",
		"Get-FileHash",
		"GITHUB_PATH",
	} {
		if !strings.Contains(install, required) {
			t.Errorf("Windows WABT installer is missing %q", required)
		}
	}
}

func TestDocsChangesRunDocumentationValidation(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(workflow)
	for _, required := range []string{
		`docs: ${{ steps.derive.outputs.docs }}`,
		`if: needs.changes.outputs.docs == 'true' || needs.changes.outputs.code == 'true'`,
		`run: make docs-check`,
		`needs: [changes, docs, lint, regression-corpus`,
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("CI workflow is missing docs-validation policy %q", required)
		}
	}
}
