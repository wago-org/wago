package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPullRequestLifecycleProfiles(t *testing.T) {
	pr := event{}
	pr.PullRequest.Base.Ref = "main"
	pr.PullRequest.Draft = true
	draft, err := makePlan("pull_request", pr, []string{"src/wago/runtime.go"}, "head-sha")
	if err != nil || draft.Profile != "smoke" || !reflect.DeepEqual(draft.ExpectedJobs, []string{"smoke"}) {
		t.Fatalf("draft plan = %+v, %v", draft, err)
	}

	pr.PullRequest.Draft = false
	ready, err := makePlan("pull_request", pr, []string{"src/wago/runtime.go"}, "head-sha")
	if err != nil || ready.Profile != "full" || !slicesContain(ready.ExpectedJobs, "app-corpus-verify") || !slicesContain(ready.ExpectedJobs, "corpus-correctness") || !slicesContain(ready.ExpectedJobs, "spec-v1") {
		t.Fatalf("ready plan = %+v, %v", ready, err)
	}

	pr.PullRequest.Draft = true // converted_to_draft uses the same event payload.
	converted, err := makePlan("pull_request", pr, []string{"src/wago/runtime.go"}, "head-sha")
	if err != nil || converted.Profile != "smoke" {
		t.Fatalf("converted draft plan = %+v, %v", converted, err)
	}
}

func TestDocumentationAndReleasePathProfiles(t *testing.T) {
	pr := event{}
	pr.PullRequest.Base.Ref = "main"
	for _, path := range []string{"README.md", "docs/quickstart.md", "LICENSE"} {
		p, err := makePlan("pull_request", pr, []string{path}, "head-sha")
		if err != nil || p.Profile != "docs" || p.Code {
			t.Fatalf("documentation path %q plan = %+v, %v", path, p, err)
		}
	}
	unknown, err := makePlan("pull_request", pr, []string{"new/unclassified.asset"}, "head-sha")
	if err != nil || unknown.Profile != "full" || !unknown.Code {
		t.Fatalf("unknown path plan = %+v, %v", unknown, err)
	}
	mainPush, err := makePlan("push", event{}, []string{"README.md"}, "main-sha")
	if err != nil || mainPush.Profile != "full" || !mainPush.Code {
		t.Fatalf("main docs push plan = %+v, %v", mainPush, err)
	}
	initialPush, err := makePlan("push", event{}, nil, "initial-main-sha")
	if err != nil || initialPush.Profile != "full" || !initialPush.Code {
		t.Fatalf("initial main push plan = %+v, %v", initialPush, err)
	}
	dispatch, err := makePlan("workflow_dispatch", event{}, nil, "release-sha")
	if err != nil || dispatch.Profile != "full" || !dispatch.CorpusSources {
		t.Fatalf("manual release plan = %+v, %v", dispatch, err)
	}
	regen, err := makePlan("pull_request", pr, []string{"tests/corpus/catalog.json"}, "head-sha")
	if err != nil || !regen.CorpusSources || !slicesContain(regen.ExpectedJobs, "regression-rebuild") {
		t.Fatalf("corpus source plan = %+v, %v", regen, err)
	}
}

func TestChangedPathsUseUpdatedPullRequestBase(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "CI Test")
	runGit(t, root, "config", "user.email", "ci@example.invalid")
	writeCommit(t, root, "README.md", "base one\n", "base one")
	baseOne := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	runGit(t, root, "checkout", "-b", "pull-request")
	writeCommit(t, root, "src/change.go", "package change\n", "pull request")
	head := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	runGit(t, root, "checkout", "main")
	writeCommit(t, root, "CONTRIBUTING.md", "updated\n", "base moved")
	baseMoved := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	if baseOne == baseMoved {
		t.Fatal("test did not advance the PR base")
	}
	pr := event{}
	pr.PullRequest.Base.Ref = "main"
	pr.PullRequest.Base.SHA = baseMoved
	pr.PullRequest.Head.SHA = head
	paths, source, err := changedPathsAt(root, "pull_request", pr, "refs/pull/1/merge")
	if err != nil {
		t.Fatal(err)
	}
	if source != head || !reflect.DeepEqual(paths, []string{"src/change.go"}) {
		t.Fatalf("base-aware diff = %v, source=%s", paths, source)
	}
}

func TestChangeDetectionFailuresDoNotSelectEmptyWork(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "CI Test")
	runGit(t, root, "config", "user.email", "ci@example.invalid")
	writeCommit(t, root, "src/a.go", "package a\n", "initial")
	sha := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	if _, err := gitDiffNamesAt(root, "missing..."+sha); err == nil {
		t.Fatal("unavailable base was accepted")
	}
	if _, err := gitDiffNamesAt(root, sha+"..."+sha); err == nil {
		t.Fatal("empty diff was accepted")
	}
	pr := event{}
	pr.PullRequest.Base.Ref = "release"
	if _, _, err := changedPathsAt(root, "pull_request", pr, "refs/pull/1/merge"); err == nil {
		t.Fatal("unexpected base branch was accepted")
	}
}

func TestAggregateRequiresEveryExpectedJob(t *testing.T) {
	results := map[string]jobState{"smoke": {Result: "success"}}
	if err := verifyResults("smoke", true, false, results); err != nil {
		t.Fatal(err)
	}
	delete(results, "smoke")
	if err := verifyResults("smoke", true, false, results); err == nil || !strings.Contains(err.Error(), "smoke is missing") {
		t.Fatalf("missing smoke result error = %v", err)
	}
	results["smoke"] = jobState{Result: "skipped"}
	if err := verifyResults("smoke", true, false, results); err == nil {
		t.Fatal("skipped expected smoke job was accepted")
	}
	fullResults := make(map[string]jobState)
	for _, id := range planFor("full", true, true, false, "").ExpectedJobs {
		fullResults[id] = jobState{Result: "success"}
	}
	delete(fullResults, "app-corpus-verify")
	if err := verifyResults("full", true, false, fullResults); err == nil || !strings.Contains(err.Error(), "app-corpus-verify is missing") {
		t.Fatalf("incomplete full result error = %v", err)
	}
}

func writeCommit(t *testing.T, root, path, contents, message string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", path)
	runGit(t, root, "commit", "-m", message)
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
