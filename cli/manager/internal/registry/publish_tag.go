package registry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/tui"
)

var errPublishCancelled = errors.New("publication cancelled")

type publishTagUI struct {
	interactive bool
	choose      func(string, []tui.Item) (string, bool)
	out         io.Writer
	now         func() time.Time
}

func defaultPublishTagUI() publishTagUI {
	return publishTagUI{
		interactive: tui.StdinIsTTY() && !automation.NoInput(),
		choose:      tui.Choose,
		out:         os.Stdout,
		now:         time.Now,
	}
}

func resolvePublishTag(ctx context.Context, root, manifestPath, version string, ui publishTagUI) (string, error) {
	localCommit, err := localTagCommit(ctx, root, version)
	if err != nil {
		return "", err
	}
	remoteCommit, remoteFound, err := remoteTagCommit(ctx, root, version)
	if err != nil {
		return "", err
	}
	if localCommit == "" && remoteFound {
		if _, err := runGit(ctx, root, "fetch", "--no-tags", "origin", "refs/tags/"+version+":refs/tags/"+version); err != nil {
			return "", fmt.Errorf("fetch release tag %s: %w", version, err)
		}
		localCommit, err = localTagCommit(ctx, root, version)
		if err != nil {
			return "", err
		}
	}
	if localCommit != "" && remoteFound && localCommit != remoteCommit {
		return "", fmt.Errorf("local tag %s points to %s but origin points to %s; refusing to move a release tag", version, shortCommit(localCommit), shortCommit(remoteCommit))
	}
	if localCommit != "" {
		return useExistingPublishTag(ctx, root, version, localCommit, remoteFound, ui)
	}
	if !ui.interactive {
		return "", errors.New(unresolvedReleaseTagInstructions(version))
	}
	return createPublishTag(ctx, root, manifestPath, version, ui)
}

func useExistingPublishTag(ctx context.Context, root, version, commit string, remoteFound bool, ui publishTagUI) (string, error) {
	age := commitAge(ctx, root, commit, ui.now())
	fmt.Fprintf(ui.out, "Found tag %s tied to commit %s (updated %s).\n", version, shortCommit(commit), age)
	if ui.interactive && !chooseYes(ui, "Use this release tag?", "Use "+version, "Cancel publication") {
		return "", errPublishCancelled
	}
	if remoteFound {
		return commit, nil
	}
	if !ui.interactive {
		return "", fmt.Errorf("tag %s exists locally but not on origin; run `git push origin refs/tags/%s`", version, version)
	}
	if !chooseYes(ui, "Tag "+version+" is not on origin. Push it now?", "Push tag", "Cancel publication") {
		return "", errPublishCancelled
	}
	if _, err := runGit(ctx, root, "push", "origin", "refs/tags/"+version); err != nil {
		return "", fmt.Errorf("push release tag %s: %w", version, err)
	}
	fmt.Fprintf(ui.out, "Pushed tag %s to origin.\n", version)
	return commit, nil
}

func createPublishTag(ctx context.Context, root, manifestPath, version string, ui publishTagUI) (string, error) {
	if !chooseYes(ui, "No release tag "+version+" was found. Create it?", "Create tag", "Cancel publication") {
		return "", errPublishCancelled
	}
	items, err := publishBranchItems(ctx, root, ui.now())
	if err != nil {
		return "", err
	}
	selected, ok := ui.choose("Create "+version+" from which branch?", items)
	if !ok {
		return "", errPublishCancelled
	}
	commit, err := resolveCommit(ctx, root, selected)
	if err != nil {
		return "", err
	}
	if err := verifyReleaseFilesAtCommit(ctx, root, manifestPath, commit); err != nil {
		return "", err
	}
	label := selected
	for _, item := range items {
		if item.Value == selected {
			label = item.Label
			break
		}
	}
	title := fmt.Sprintf("Create and push %s from %s at %s?", version, label, shortCommit(commit))
	if !chooseYes(ui, title, "Create and push tag", "Cancel publication") {
		return "", errPublishCancelled
	}
	if _, err := runGit(ctx, root, "tag", version, commit); err != nil {
		return "", fmt.Errorf("create release tag %s: %w", version, err)
	}
	if _, err := runGit(ctx, root, "push", "origin", "refs/tags/"+version); err != nil {
		return "", fmt.Errorf("created local tag %s, but push failed: %w; retry with `git push origin refs/tags/%s`", version, err, version)
	}
	fmt.Fprintf(ui.out, "Created and pushed tag %s at %s.\n", version, shortCommit(commit))
	return commit, nil
}

func chooseYes(ui publishTagUI, title, yesDescription, noDescription string) bool {
	selected, ok := ui.choose(title, []tui.Item{
		{Label: "Yes", Description: yesDescription, Value: "yes"},
		{Label: "No", Description: noDescription, Value: "no"},
	})
	return ok && selected == "yes"
}

func publishBranchItems(ctx context.Context, root string, now time.Time) ([]tui.Item, error) {
	current := strings.TrimSpace(gitOutputAt(root, "branch", "--show-current"))
	defaultBranch := publishDefaultBranch(ctx, root)
	output, err := runGit(ctx, root, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, fmt.Errorf("list release branches: %w", err)
	}
	branches := strings.Fields(output)
	defaultRef := defaultBranch
	defaultIsLocal := false
	for _, branch := range branches {
		if branch == defaultBranch {
			defaultIsLocal = true
			break
		}
	}
	if !defaultIsLocal && defaultBranch != "" {
		remoteRef := "origin/" + defaultBranch
		if commit := strings.TrimSpace(gitOutputAt(root, "show-ref", "--verify", "--hash", "refs/remotes/"+remoteRef)); commit != "" {
			branches = append(branches, remoteRef)
			defaultRef = remoteRef
		}
	}
	if len(branches) == 0 {
		return nil, errors.New("no local or fetched origin branches are available for the release tag")
	}
	sort.Strings(branches)
	ordered := make([]string, 0, len(branches))
	appendBranch := func(branch string) {
		if branch == "" {
			return
		}
		for _, existing := range ordered {
			if existing == branch {
				return
			}
		}
		for _, available := range branches {
			if available == branch {
				ordered = append(ordered, branch)
				return
			}
		}
	}
	appendBranch(defaultRef)
	appendBranch(current)
	for _, branch := range branches {
		appendBranch(branch)
	}
	items := make([]tui.Item, 0, len(ordered))
	for _, branch := range ordered {
		commit, err := resolveCommit(ctx, root, branch)
		if err != nil {
			return nil, err
		}
		label := branch
		if branch == defaultRef {
			label = defaultBranch + " (default)"
			if !defaultIsLocal {
				label = defaultBranch + " (default, origin)"
			}
		} else if branch == current {
			label += " (current)"
		}
		items = append(items, tui.Item{
			Label:       label,
			Description: shortCommit(commit) + " · updated " + commitAge(ctx, root, commit, now),
			Value:       branch,
		})
	}
	return items, nil
}

func publishDefaultBranch(ctx context.Context, root string) string {
	if ref := strings.TrimSpace(gitOutputAt(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")); strings.HasPrefix(ref, "origin/") {
		return strings.TrimPrefix(ref, "origin/")
	}
	output, err := runGit(ctx, root, "ls-remote", "--symref", "origin", "HEAD")
	if err == nil {
		for _, line := range strings.Split(output, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 3 && fields[0] == "ref:" && fields[2] == "HEAD" && strings.HasPrefix(fields[1], "refs/heads/") {
				return strings.TrimPrefix(fields[1], "refs/heads/")
			}
		}
	}
	if main := strings.TrimSpace(gitOutputAt(root, "show-ref", "--verify", "--hash", "refs/heads/main")); main != "" {
		return "main"
	}
	return strings.TrimSpace(gitOutputAt(root, "branch", "--show-current"))
}

func localTagCommit(ctx context.Context, root, version string) (string, error) {
	output, err := runGit(ctx, root, "rev-parse", "--verify", "--quiet", "refs/tags/"+version+"^{commit}")
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("resolve local release tag %s: %w", version, err)
	}
	commit := strings.TrimSpace(output)
	if !fullGitCommit(commit) {
		return "", fmt.Errorf("tag %s did not resolve to a full commit", version)
	}
	return commit, nil
}

func remoteTagCommit(ctx context.Context, root, version string) (string, bool, error) {
	output, err := runGit(ctx, root, "ls-remote", "--exit-code", "--tags", "origin", "refs/tags/"+version, "refs/tags/"+version+"^{}")
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 2 {
			return "", false, nil
		}
		return "", false, fmt.Errorf("check origin for release tag %s: %w", version, err)
	}
	var commit string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			commit = fields[0]
			if strings.HasSuffix(fields[1], "^{}") {
				break
			}
		}
	}
	if !fullGitCommit(commit) {
		return "", false, fmt.Errorf("origin tag %s did not resolve to a full commit", version)
	}
	return commit, true, nil
}

func resolveCommit(ctx context.Context, root, ref string) (string, error) {
	output, err := runGit(ctx, root, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve release branch %s: %w", ref, err)
	}
	commit := strings.TrimSpace(output)
	if !fullGitCommit(commit) {
		return "", fmt.Errorf("release branch %s did not resolve to a full commit", ref)
	}
	return commit, nil
}

func verifyReleaseFilesAtCommit(ctx context.Context, root, manifestPath, commit string) error {
	status, err := runGit(ctx, root, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect release checkout: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("release checkout has uncommitted or untracked files; commit or remove them before creating an immutable release tag")
	}
	if _, err := runGit(ctx, root, "diff", "--quiet", commit, "--", "."); err != nil {
		return fmt.Errorf("commit %s does not match the current release checkout; switch to the selected branch, prepare and commit the release, then run publish again", shortCommit(commit))
	}
	prefix := strings.TrimSpace(gitOutputAt(root, "rev-parse", "--show-prefix"))
	localManifestPath := manifestPath
	if !filepath.IsAbs(localManifestPath) {
		localManifestPath = filepath.Join(root, filepath.Base(manifestPath))
	}
	files := []struct {
		path string
		name string
	}{
		{path: localManifestPath, name: filepath.ToSlash(filepath.Join(prefix, filepath.Base(manifestPath)))},
		{path: filepath.Join(root, wago.ProviderCatalogFile), name: filepath.ToSlash(filepath.Join(prefix, wago.ProviderCatalogFile))},
	}
	for _, file := range files {
		local, err := os.ReadFile(file.path)
		if err != nil {
			return fmt.Errorf("read release file %s: %w", file.path, err)
		}
		committed, err := runGit(ctx, root, "show", commit+":"+file.name)
		if err != nil || !bytes.Equal(local, []byte(committed)) {
			return fmt.Errorf("commit %s does not contain the current %s; commit the exact release files to the selected branch first", shortCommit(commit), filepath.Base(file.path))
		}
	}
	return nil
}

func commitAge(ctx context.Context, root, commit string, now time.Time) string {
	output, err := runGit(ctx, root, "show", "-s", "--format=%ct", commit)
	if err != nil {
		return "at an unknown time"
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if err != nil {
		return "at an unknown time"
	}
	return relativeAge(now.Sub(time.Unix(seconds, 0)))
}

func relativeAge(age time.Duration) string {
	if age < 0 {
		return "in the future"
	}
	unit := func(value int64, name string) string {
		if value == 1 {
			return "1 " + name + " ago"
		}
		return fmt.Sprintf("%d %ss ago", value, name)
	}
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return unit(int64(age/time.Minute), "minute")
	case age < 24*time.Hour:
		return unit(int64(age/time.Hour), "hour")
	case age < 14*24*time.Hour:
		return unit(int64(age/(24*time.Hour)), "day")
	case age < 60*24*time.Hour:
		return unit(int64(age/(7*24*time.Hour)), "week")
	case age < 2*365*24*time.Hour:
		return unit(int64(age/(30*24*time.Hour)), "month")
	default:
		return unit(int64(age/(365*24*time.Hour)), "year")
	}
}

func shortCommit(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}

func runGit(ctx context.Context, root string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, message)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(output), nil
}
