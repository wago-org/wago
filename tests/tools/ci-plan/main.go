// Command ci-plan selects the CI profile and verifies its expected results.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type event struct {
	Ref         string `json:"ref"`
	Before      string `json:"before"`
	After       string `json:"after"`
	PullRequest struct {
		Draft bool `json:"draft"`
		Base  struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
}

type plan struct {
	Profile       string   `json:"profile"`
	Code          bool     `json:"code"`
	Docs          bool     `json:"docs"`
	CorpusSources bool     `json:"corpus_sources"`
	SourceSHA     string   `json:"source_sha"`
	ExpectedJobs  []string `json:"expected_jobs"`
}

type jobState struct {
	Result string `json:"result"`
}

var fullJobs = []string{
	"changes", "docs", "lint", "regression-integrity", "gc-hardening", "race",
	"platform-test", "corpus-correctness", "app-corpus", "app-corpus-verify", "core-v2", "core-v3", "current-go",
	"fuzz", "tinygo", "size",
}

func main() {
	if len(os.Args) < 2 {
		fail(errors.New("usage: ci-plan plan | verify"))
	}
	var err error
	switch os.Args[1] {
	case "plan":
		err = writePlan(os.Args[2:])
	case "verify":
		err = verifyEnv()
	default:
		err = errors.New("usage: ci-plan plan | verify")
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ci-plan:", err)
	os.Exit(1)
}

func writePlan(args []string) error {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	eventPath := flags.String("event", os.Getenv("GITHUB_EVENT_PATH"), "GitHub event JSON")
	eventName := flags.String("event-name", os.Getenv("GITHUB_EVENT_NAME"), "GitHub event name")
	ref := flags.String("ref", os.Getenv("GITHUB_REF"), "GitHub ref")
	output := flags.String("output", os.Getenv("GITHUB_OUTPUT"), "GitHub output file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *eventPath == "" || *eventName == "" {
		return errors.New("GitHub event path and event name are required")
	}
	data, err := os.ReadFile(filepath.Clean(*eventPath))
	if err != nil {
		return fmt.Errorf("read GitHub event: %w", err)
	}
	var e event
	if err := json.Unmarshal(data, &e); err != nil {
		return fmt.Errorf("decode GitHub event: %w", err)
	}
	paths, sourceSHA, err := changedPaths(*eventName, e, *ref)
	if err != nil {
		return err
	}
	if testedSHA := os.Getenv("GITHUB_SHA"); testedSHA != "" {
		// GitHub checks out the pull-request merge commit by default. Keep the
		// recorded identity aligned with the source all jobs actually test.
		sourceSHA = testedSHA
	}
	p, err := makePlan(*eventName, e, paths, sourceSHA)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if *output == "" {
		_, err = os.Stdout.Write(append(encoded, '\n'))
		return err
	}
	file, err := os.OpenFile(filepath.Clean(*output), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open GitHub output: %w", err)
	}
	defer file.Close()
	for key, value := range map[string]string{
		"profile":        p.Profile,
		"code":           boolString(p.Code),
		"docs":           boolString(p.Docs),
		"corpus_sources": boolString(p.CorpusSources),
		"source_sha":     p.SourceSHA,
		"expected_jobs":  strings.Join(p.ExpectedJobs, ","),
		"plan":           string(encoded),
	} {
		if _, err := fmt.Fprintf(file, "%s=%s\n", key, value); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stdout, "CI profile: %s; code=%t docs=%t corpus-sources=%t\n", p.Profile, p.Code, p.Docs, p.CorpusSources)
	fmt.Fprintf(os.Stdout, "Expected jobs: %s\n", strings.Join(p.ExpectedJobs, ", "))
	return nil
}

func changedPaths(name string, e event, ref string) ([]string, string, error) {
	return changedPathsAt(".", name, e, ref)
}

func changedPathsAt(root, name string, e event, ref string) ([]string, string, error) {
	switch name {
	case "pull_request":
		if e.PullRequest.Base.Ref != "main" {
			return nil, "", fmt.Errorf("unexpected pull request base %q; CI only qualifies main", e.PullRequest.Base.Ref)
		}
		paths, err := gitDiffNamesAt(root, e.PullRequest.Base.SHA+"..."+e.PullRequest.Head.SHA)
		if err != nil {
			return nil, "", fmt.Errorf("detect pull request changes against base %s: %w", e.PullRequest.Base.SHA, err)
		}
		return paths, e.PullRequest.Head.SHA, nil
	case "push":
		if ref != "refs/heads/main" || e.Ref != "refs/heads/main" {
			return nil, "", fmt.Errorf("unexpected push ref %q", ref)
		}
		if e.Before == "" || e.After == "" || strings.Trim(e.Before, "0") == "" {
			return nil, "", nil // An initial or incomplete push is qualified in full.
		}
		paths, err := gitDiffNamesAt(root, e.Before+"..."+e.After)
		if err != nil {
			return nil, "", fmt.Errorf("detect pushed changes: %w", err)
		}
		return paths, e.After, nil
	case "workflow_dispatch":
		// Manual qualification is always full. It is also the release path for
		// documentation or version-only commits, so it cannot be path-gated.
		return nil, os.Getenv("GITHUB_SHA"), nil
	default:
		return nil, "", fmt.Errorf("unsupported CI event %q", name)
	}
}

func gitDiffNamesAt(root, revision string) ([]string, error) {
	if strings.ContainsAny(revision, "\n\r") || strings.Contains(revision, ".. ..") {
		return nil, errors.New("invalid revision range")
	}
	cmd := exec.Command("git", "diff", "--name-only", "--no-renames", revision)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var paths []string
	for _, path := range strings.Split(string(output), "\n") {
		if path != "" {
			paths = append(paths, filepath.ToSlash(path))
		}
	}
	if len(paths) == 0 {
		return nil, errors.New("change detection returned an empty selection")
	}
	return paths, nil
}

func makePlan(name string, e event, paths []string, sourceSHA string) (plan, error) {
	if name == "workflow_dispatch" {
		return planFor("full", true, true, true, sourceSHA), nil
	}
	if len(paths) == 0 && name == "push" {
		return planFor("full", true, true, false, sourceSHA), nil
	}
	if len(paths) == 0 {
		return plan{}, errors.New("change detection returned an empty selection")
	}
	p := plan{Docs: true, SourceSHA: sourceSHA}
	for _, path := range paths {
		if !documentationPath(path) {
			p.Code = true
		}
		if corpusSourcePath(path) {
			p.CorpusSources = true
		}
	}
	if name == "push" {
		// Main commits always produce release-usable, exact-SHA evidence, even
		// when their diff only changes docs or version metadata.
		return planFor("full", true, true, p.CorpusSources, sourceSHA), nil
	}
	if name != "pull_request" {
		return plan{}, fmt.Errorf("unsupported CI event %q", name)
	}
	if e.PullRequest.Draft {
		return planFor("smoke", p.Code, true, false, sourceSHA), nil
	}
	if !p.Code {
		return planFor("docs", false, true, false, sourceSHA), nil
	}
	p = planFor("full", true, true, p.CorpusSources, sourceSHA)
	return p, nil
}

func planFor(profile string, code, docs, corpusSources bool, sourceSHA string) plan {
	jobs := []string{"changes"}
	switch profile {
	case "smoke":
		jobs = []string{"smoke"}
	case "docs":
		jobs = append(jobs, "docs")
	case "full":
		jobs = append(jobs, fullJobs[1:]...)
		if corpusSources {
			jobs = append(jobs, "regression-rebuild")
		}
	}
	slices.Sort(jobs)
	return plan{Profile: profile, Code: code, Docs: docs, CorpusSources: corpusSources, SourceSHA: sourceSHA, ExpectedJobs: jobs}
}

func documentationPath(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	return path == "LICENSE" || strings.HasPrefix(path, "docs/") || strings.HasSuffix(path, ".md")
}

func corpusSourcePath(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	for _, prefix := range []string{
		"tests/corpus/", "tests/tools/regression-corpus/", "tests/support/regressioncorpus/",
		"tests/scripts/verify-regression-corpus", "tests/scripts/install-wabt-windows.ps1",
		"tests/conformance/spec-v3", "scripts/bootstrap-wabt.sh", "scripts/bootstrap-spec-interpreter.sh",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return path == ".gitmodules" || path == "go.mod" || path == "go.sum"
}

func verifyEnv() error {
	profile := os.Getenv("CI_PROFILE")
	if profile == "" {
		return errors.New("CI_PROFILE is required")
	}
	code := os.Getenv("CI_CODE") == "true"
	corpusSources := os.Getenv("CI_CORPUS_SOURCES") == "true"
	var results map[string]jobState
	if err := json.Unmarshal([]byte(os.Getenv("CI_NEEDS")), &results); err != nil {
		return fmt.Errorf("decode CI job results: %w", err)
	}
	return verifyResults(profile, code, corpusSources, results)
}

func verifyResults(profile string, code, corpusSources bool, results map[string]jobState) error {
	var expected []string
	switch profile {
	case "smoke":
		expected = []string{"smoke"}
	case "docs":
		expected = []string{"changes", "docs"}
	case "full":
		if !code {
			return errors.New("full qualification has an empty code selection")
		}
		expected = planFor("full", true, true, corpusSources, "").ExpectedJobs
	default:
		return fmt.Errorf("unknown CI profile %q", profile)
	}
	for _, id := range expected {
		job, ok := results[id]
		if !ok {
			return fmt.Errorf("expected CI job %s is missing", id)
		}
		if job.Result != "success" {
			return fmt.Errorf("expected CI job %s did not succeed: %s", id, job.Result)
		}
	}
	return nil
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
