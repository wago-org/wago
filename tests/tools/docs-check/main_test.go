package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFilesAcceptsValidLocalLinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "README.md", strings.Join([]string{
		"# Home",
		"[guide](docs/guide.md#details)",
		"[same](#home)",
		"[source](src/example.go)",
		"[reference]: docs/guide.md#details",
		`<a href="LICENSE">license</a>`,
	}, "\n"))
	writeFile(t, root, "docs/guide.md", "# Details\n")
	writeFile(t, root, "src/example.go", "package example\n")
	writeFile(t, root, "LICENSE", "test\n")

	if problems := checkFiles(root, []string{"README.md", "docs/guide.md"}); len(problems) != 0 {
		t.Fatalf("unexpected problems: %+v", problems)
	}
}

func TestCheckFilesRejectsBrokenPathAnchorAndCase(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "README.md", strings.Join([]string{
		"[missing](docs/missing.md)",
		"[anchor](docs/guide.md#missing)",
		"[case](Docs/guide.md)",
	}, "\n"))
	writeFile(t, root, "docs/guide.md", "# Present\n")

	problems := checkFiles(root, []string{"README.md", "docs/guide.md"})
	if len(problems) != 3 {
		t.Fatalf("got %d problems, want 3: %+v", len(problems), problems)
	}
	joined := problems[0].message + "\n" + problems[1].message + "\n" + problems[2].message
	for _, want := range []string{"file does not exist", "missing Markdown anchor", "path case mismatch"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems do not contain %q: %s", want, joined)
		}
	}
}

func TestExtractLinksSkipsCodeAndExternalLinks(t *testing.T) {
	markdown := strings.Join([]string{
		"[local](guide.md)",
		"`[inline](ignored.md)`",
		"```md",
		"[fenced](ignored.md)",
		"```",
		"[web](https://example.com/missing)",
	}, "\n")
	links := extractLinks(markdown)
	if len(links) != 2 {
		t.Fatalf("got links %+v", links)
	}
	if links[0].destination != "guide.md" || links[1].destination != "https://example.com/missing" {
		t.Fatalf("unexpected links: %+v", links)
	}
}

func TestMarkdownAnchorsMatchGitHubDuplicates(t *testing.T) {
	anchors := markdownAnchors("# Review Focus\n## Review Focus\n")
	for _, want := range []string{"review-focus", "review-focus-1"} {
		if _, ok := anchors[want]; !ok {
			t.Errorf("missing anchor %q in %+v", want, anchors)
		}
	}
}

func writeFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
