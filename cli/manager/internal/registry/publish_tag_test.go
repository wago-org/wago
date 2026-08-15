package registry

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/cli/internal/tui"
)

func TestResolvePublishTagConfirmsExistingTagWithCommitAndAge(t *testing.T) {
	repository, _, commit := newPublishTagRepository(t)
	testGit(t, repository, "tag", "v1.2.3", commit)
	testGit(t, repository, "push", "origin", "refs/tags/v1.2.3")
	var output bytes.Buffer
	var titles []string
	ui := publishTagUI{
		interactive: true,
		choose: func(title string, items []tui.Item) (string, bool) {
			titles = append(titles, title)
			if len(items) != 2 || items[0].Value != "yes" || items[1].Value != "no" {
				t.Fatalf("confirmation items = %#v", items)
			}
			return "yes", true
		},
		out: &output,
		now: time.Now,
	}

	got, err := resolvePublishTag(context.Background(), repository, filepath.Join(repository, "wago.json"), "v1.2.3", ui)
	if err != nil || got != commit {
		t.Fatalf("resolve tag = %q, %v; want %q", got, err, commit)
	}
	if len(titles) != 1 || titles[0] != "Use this release tag?" {
		t.Fatalf("confirmation titles = %q", titles)
	}
	for _, want := range []string{"Found tag v1.2.3", shortCommit(commit), "updated"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("tag summary missing %q:\n%s", want, output.String())
		}
	}
}

func TestResolvePublishTagCreatesAndPushesFromDefaultBranch(t *testing.T) {
	repository, remote, commit := newPublishTagRepository(t)
	var output bytes.Buffer
	ui := publishTagUI{
		interactive: true,
		choose: func(title string, items []tui.Item) (string, bool) {
			switch {
			case strings.HasPrefix(title, "No release tag"):
				return "yes", true
			case strings.HasPrefix(title, "Create v1.2.3 from"):
				if len(items) == 0 || items[0].Label != "main (default)" || items[0].Value != "main" {
					t.Fatalf("branch items = %#v", items)
				}
				return items[0].Value, true
			case strings.HasPrefix(title, "Create and push"):
				return "yes", true
			default:
				t.Fatalf("unexpected prompt %q", title)
				return "", false
			}
		},
		out: &output,
		now: time.Now,
	}

	got, err := resolvePublishTag(context.Background(), repository, filepath.Join(repository, "wago.json"), "v1.2.3", ui)
	if err != nil || got != commit {
		t.Fatalf("resolve tag = %q, %v; want %q", got, err, commit)
	}
	remoteCommit := strings.TrimSpace(testGit(t, remote, "rev-parse", "refs/tags/v1.2.3^{commit}"))
	if remoteCommit != commit {
		t.Fatalf("remote tag = %q, want %q", remoteCommit, commit)
	}
	if !strings.Contains(output.String(), "Created and pushed tag v1.2.3 at "+shortCommit(commit)) {
		t.Fatalf("creation output = %q", output.String())
	}
}

func TestResolvePublishTagFetchesAnExistingRemoteTag(t *testing.T) {
	repository, _, commit := newPublishTagRepository(t)
	testGit(t, repository, "tag", "v1.2.3", commit)
	testGit(t, repository, "push", "origin", "refs/tags/v1.2.3")
	testGit(t, repository, "tag", "-d", "v1.2.3")
	ui := publishTagUI{
		interactive: true,
		choose:      func(string, []tui.Item) (string, bool) { return "yes", true },
		out:         &bytes.Buffer{},
		now:         time.Now,
	}

	got, err := resolvePublishTag(context.Background(), repository, filepath.Join(repository, "wago.json"), "v1.2.3", ui)
	if err != nil || got != commit {
		t.Fatalf("resolve remote tag = %q, %v; want %q", got, err, commit)
	}
	if local := strings.TrimSpace(testGit(t, repository, "rev-parse", "refs/tags/v1.2.3^{commit}")); local != commit {
		t.Fatalf("fetched local tag = %q, want %q", local, commit)
	}
}

func TestResolvePublishTagOffersToPushAnExistingLocalTag(t *testing.T) {
	repository, remote, commit := newPublishTagRepository(t)
	testGit(t, repository, "tag", "v1.2.3", commit)
	var titles []string
	ui := publishTagUI{
		interactive: true,
		choose: func(title string, _ []tui.Item) (string, bool) {
			titles = append(titles, title)
			return "yes", true
		},
		out: &bytes.Buffer{},
		now: time.Now,
	}

	got, err := resolvePublishTag(context.Background(), repository, filepath.Join(repository, "wago.json"), "v1.2.3", ui)
	if err != nil || got != commit {
		t.Fatalf("resolve local tag = %q, %v; want %q", got, err, commit)
	}
	if len(titles) != 2 || titles[0] != "Use this release tag?" || !strings.Contains(titles[1], "Push it now?") {
		t.Fatalf("confirmation titles = %q", titles)
	}
	if pushed := strings.TrimSpace(testGit(t, remote, "rev-parse", "refs/tags/v1.2.3^{commit}")); pushed != commit {
		t.Fatalf("pushed tag = %q, want %q", pushed, commit)
	}
}

func TestResolvePublishTagCanChooseCurrentBranchInsteadOfDefault(t *testing.T) {
	repository, remote, mainCommit := newPublishTagRepository(t)
	testGit(t, repository, "switch", "-c", "release-candidate")
	writePublishReleaseFiles(t, repository, "1.2.3-release")
	testGit(t, repository, "add", "wago.json", "wago.providers.json")
	testGit(t, repository, "commit", "-m", "prepare release")
	releaseCommit := strings.TrimSpace(testGit(t, repository, "rev-parse", "HEAD"))
	if releaseCommit == mainCommit {
		t.Fatal("release branch did not advance")
	}
	ui := publishTagUI{
		interactive: true,
		choose: func(title string, items []tui.Item) (string, bool) {
			switch {
			case strings.HasPrefix(title, "No release tag"), strings.HasPrefix(title, "Create and push"):
				return "yes", true
			case strings.HasPrefix(title, "Create v1.2.3 from"):
				if len(items) < 2 || items[0].Label != "main (default)" || items[1].Label != "release-candidate (current)" {
					t.Fatalf("branch ordering = %#v", items)
				}
				return "release-candidate", true
			default:
				return "", false
			}
		},
		out: &bytes.Buffer{},
		now: time.Now,
	}

	got, err := resolvePublishTag(context.Background(), repository, filepath.Join(repository, "wago.json"), "v1.2.3", ui)
	if err != nil || got != releaseCommit {
		t.Fatalf("resolve tag = %q, %v; want %q", got, err, releaseCommit)
	}
	remoteCommit := strings.TrimSpace(testGit(t, remote, "rev-parse", "refs/tags/v1.2.3^{commit}"))
	if remoteCommit != releaseCommit {
		t.Fatalf("remote tag = %q, want release branch %q", remoteCommit, releaseCommit)
	}
}

func TestResolvePublishTagRejectsBranchWithoutCurrentReleaseFiles(t *testing.T) {
	repository, _, _ := newPublishTagRepository(t)
	testGit(t, repository, "switch", "-c", "release-candidate")
	writePublishReleaseFiles(t, repository, "1.2.3-release")
	testGit(t, repository, "add", "wago.json", "wago.providers.json")
	testGit(t, repository, "commit", "-m", "prepare release")
	ui := publishTagUI{
		interactive: true,
		choose: func(title string, _ []tui.Item) (string, bool) {
			switch {
			case strings.HasPrefix(title, "No release tag"), strings.HasPrefix(title, "Create and push"):
				return "yes", true
			case strings.HasPrefix(title, "Create v1.2.3 from"):
				return "main", true
			default:
				return "", false
			}
		},
		out: &bytes.Buffer{},
		now: time.Now,
	}

	_, err := resolvePublishTag(context.Background(), repository, filepath.Join(repository, "wago.json"), "v1.2.3", ui)
	if err == nil || !strings.Contains(err.Error(), "does not match the current release checkout") {
		t.Fatalf("mismatched branch error = %v", err)
	}
	if tag := strings.TrimSpace(gitOutputAt(repository, "tag", "--list", "v1.2.3")); tag != "" {
		t.Fatalf("mismatched branch created tag %q", tag)
	}
}

func TestResolvePublishTagRejectsDirtyReleaseCheckout(t *testing.T) {
	repository, _, _ := newPublishTagRepository(t)
	if err := os.WriteFile(filepath.Join(repository, "untracked.go"), []byte("package plugin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ui := publishTagUI{
		interactive: true,
		choose: func(title string, items []tui.Item) (string, bool) {
			switch {
			case strings.HasPrefix(title, "No release tag"), strings.HasPrefix(title, "Create and push"):
				return "yes", true
			case strings.HasPrefix(title, "Create v1.2.3 from"):
				return items[0].Value, true
			default:
				return "", false
			}
		},
		out: &bytes.Buffer{},
		now: time.Now,
	}

	_, err := resolvePublishTag(context.Background(), repository, filepath.Join(repository, "wago.json"), "v1.2.3", ui)
	if err == nil || !strings.Contains(err.Error(), "uncommitted or untracked files") {
		t.Fatalf("dirty checkout error = %v", err)
	}
	if tag := strings.TrimSpace(gitOutputAt(repository, "tag", "--list", "v1.2.3")); tag != "" {
		t.Fatalf("dirty checkout created tag %q", tag)
	}
}

func TestResolvePublishTagCancellationAndNonInteractiveInstructions(t *testing.T) {
	repository, _, _ := newPublishTagRepository(t)
	ui := publishTagUI{
		interactive: true,
		choose:      func(string, []tui.Item) (string, bool) { return "no", true },
		out:         &bytes.Buffer{},
		now:         time.Now,
	}
	if _, err := resolvePublishTag(context.Background(), repository, filepath.Join(repository, "wago.json"), "v1.2.3", ui); !errors.Is(err, errPublishCancelled) {
		t.Fatalf("cancel error = %v", err)
	}
	if tag := strings.TrimSpace(gitOutputAt(repository, "tag", "--list", "v1.2.3")); tag != "" {
		t.Fatalf("cancellation created tag %q", tag)
	}

	ui.interactive = false
	_, err := resolvePublishTag(context.Background(), repository, filepath.Join(repository, "wago.json"), "v1.2.3", ui)
	if err == nil || !strings.Contains(err.Error(), "git fetch origin tag v1.2.3") || !strings.Contains(err.Error(), "git tag v1.2.3") {
		t.Fatalf("non-interactive error = %v", err)
	}
}

func TestRelativeAge(t *testing.T) {
	for _, test := range []struct {
		age  time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{time.Minute, "1 minute ago"},
		{2 * time.Hour, "2 hours ago"},
		{3 * 24 * time.Hour, "3 days ago"},
		{21 * 24 * time.Hour, "3 weeks ago"},
		{-time.Second, "in the future"},
	} {
		if got := relativeAge(test.age); got != test.want {
			t.Errorf("relativeAge(%v) = %q, want %q", test.age, got, test.want)
		}
	}
}

func newPublishTagRepository(t *testing.T) (repository, remote, commit string) {
	t.Helper()
	repository = filepath.Join(t.TempDir(), "work")
	remote = filepath.Join(t.TempDir(), "remote.git")
	testGit(t, "", "init", "--bare", "--initial-branch=main", remote)
	testGit(t, "", "init", "--initial-branch=main", repository)
	testGit(t, repository, "config", "user.name", "Wago test")
	testGit(t, repository, "config", "user.email", "wago@example.test")
	writePublishReleaseFiles(t, repository, "1.2.3")
	testGit(t, repository, "add", "wago.json", "wago.providers.json")
	testGit(t, repository, "commit", "-m", "release files")
	testGit(t, repository, "remote", "add", "origin", remote)
	testGit(t, repository, "push", "-u", "origin", "main")
	commit = strings.TrimSpace(testGit(t, repository, "rev-parse", "HEAD"))
	return
}

func writePublishReleaseFiles(t *testing.T, repository, value string) {
	t.Helper()
	for name, contents := range map[string]string{
		"wago.json":           `{"package":{"version":"` + value + `"}}` + "\n",
		"wago.providers.json": `{"providers":[]}` + "\n",
	} {
		if err := os.WriteFile(filepath.Join(repository, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func testGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
