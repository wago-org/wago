// Command release-qualification creates and verifies the immutable records used
// to authorize stable Wago publication.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	assetPattern   = regexp.MustCompile(`^wago-[A-Za-z0-9._-]+$`)
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

var requiredJobs = []string{
	"changes", "docs", "lint", "regression-corpus", "runtime-concurrency",
	"race", "platform-test", "core-v2", "core-v3", "tinygo", "coverage", "size",
}

type qualificationJob struct {
	ID     string `json:"id"`
	Result string `json:"result"`
}

type qualification struct {
	Schema      int                `json:"schema"`
	Repository  string             `json:"repository"`
	SourceSHA   string             `json:"source_sha"`
	WorkflowSHA string             `json:"workflow_sha"`
	RunID       int64              `json:"run_id"`
	RunAttempt  int64              `json:"run_attempt"`
	Event       string             `json:"event"`
	Ref         string             `json:"ref"`
	WorkflowRef string             `json:"workflow_ref"`
	Jobs        []qualificationJob `json:"jobs"`
}

type releaseAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type releaseManifest struct {
	Schema        int            `json:"schema"`
	Version       string         `json:"version"`
	SourceSHA     string         `json:"source_sha"`
	CIRunID       int64          `json:"ci_run_id"`
	CIRunAttempt  int64          `json:"ci_run_attempt"`
	Qualification qualification  `json:"qualification"`
	Assets        []releaseAsset `json:"assets"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "release qualification:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "record-ci":
		if len(args) != 2 {
			return usageError()
		}
		return recordCI(args[1])
	case "verify-ci":
		if len(args) != 6 {
			return usageError()
		}
		runID, err := positiveInt(args[4], "CI run ID")
		if err != nil {
			return err
		}
		runAttempt, err := positiveInt(args[5], "CI run attempt")
		if err != nil {
			return err
		}
		q, err := readQualification(args[1])
		if err != nil {
			return err
		}
		return verifyQualification(q, args[2], args[3], runID, runAttempt)
	case "create-release":
		if len(args) != 8 {
			return usageError()
		}
		runID, err := positiveInt(args[4], "CI run ID")
		if err != nil {
			return err
		}
		runAttempt, err := positiveInt(args[5], "CI run attempt")
		if err != nil {
			return err
		}
		return createRelease(args[1], args[2], args[3], runID, runAttempt, args[6], args[7])
	case "verify-release":
		if len(args) != 8 {
			return usageError()
		}
		runID, err := positiveInt(args[6], "CI run ID")
		if err != nil {
			return err
		}
		runAttempt, err := positiveInt(args[7], "CI run attempt")
		if err != nil {
			return err
		}
		return verifyRelease(args[1], args[2], args[3], args[4], args[5], runID, runAttempt)
	default:
		return usageError()
	}
}

func usageError() error {
	return errors.New("usage: record-ci <output> | verify-ci <manifest> <repository> <source-sha> <run-id> <run-attempt> | create-release <release-dir> <version> <source-sha> <run-id> <run-attempt> <ci-manifest> <output> | verify-release <manifest> <release-dir> <repository> <version> <source-sha> <run-id> <run-attempt>")
}

func positiveInt(value, name string) (int64, error) {
	result, err := strconv.ParseInt(value, 10, 64)
	if err != nil || result < 1 {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	return result, nil
}

func recordCI(output string) error {
	runID, err := positiveInt(os.Getenv("CI_RUN_ID"), "CI run ID")
	if err != nil {
		return err
	}
	runAttempt, err := positiveInt(os.Getenv("CI_RUN_ATTEMPT"), "CI run attempt")
	if err != nil {
		return err
	}
	var needs map[string]struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(os.Getenv("CI_NEEDS")), &needs); err != nil {
		return fmt.Errorf("decode CI needs: %w", err)
	}
	jobs := make([]qualificationJob, 0, len(needs))
	for id, need := range needs {
		jobs = append(jobs, qualificationJob{ID: id, Result: need.Result})
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
	q := qualification{
		Schema: 1, Repository: os.Getenv("CI_REPOSITORY"),
		SourceSHA: os.Getenv("CI_SOURCE_SHA"), WorkflowSHA: os.Getenv("CI_SOURCE_SHA"),
		RunID: runID, RunAttempt: runAttempt, Event: "push", Ref: "refs/heads/main",
		WorkflowRef: os.Getenv("CI_WORKFLOW_REF"), Jobs: jobs,
	}
	if err := verifyQualification(q, q.Repository, q.SourceSHA, q.RunID, q.RunAttempt); err != nil {
		return err
	}
	return writeJSON(output, q)
}

func readQualification(path string) (qualification, error) {
	var q qualification
	if err := readJSON(path, &q); err != nil {
		return q, fmt.Errorf("read CI qualification: %w", err)
	}
	return q, nil
}

func verifyQualification(q qualification, repository, sourceSHA string, runID, runAttempt int64) error {
	if !shaPattern.MatchString(sourceSHA) {
		return fmt.Errorf("invalid full commit SHA %q", sourceSHA)
	}
	if q.Schema != 1 || q.Repository != repository || q.SourceSHA != sourceSHA ||
		q.WorkflowSHA != sourceSHA || q.RunID != runID || q.RunAttempt != runAttempt ||
		q.Event != "push" || q.Ref != "refs/heads/main" ||
		q.WorkflowRef != repository+"/.github/workflows/ci.yml@refs/heads/main" {
		return errors.New("CI qualification does not identify the exact successful main source run")
	}
	seen := make(map[string]bool, len(q.Jobs))
	for _, job := range q.Jobs {
		if job.ID == "" || seen[job.ID] {
			return fmt.Errorf("duplicate or empty qualified job %q", job.ID)
		}
		seen[job.ID] = true
		if job.Result != "success" {
			return fmt.Errorf("required CI job %s did not succeed: %s", job.ID, job.Result)
		}
	}
	for _, id := range requiredJobs {
		if !seen[id] {
			return fmt.Errorf("CI qualification is missing required job %s", id)
		}
	}
	return nil
}

func createRelease(releaseDir, version, sourceSHA string, runID, runAttempt int64, ciPath, output string) error {
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid stable version %q", version)
	}
	q, err := readQualification(ciPath)
	if err != nil {
		return err
	}
	repository := os.Getenv("GITHUB_REPOSITORY")
	if repository == "" {
		repository = "wago-org/wago"
	}
	if err := verifyQualification(q, repository, sourceSHA, runID, runAttempt); err != nil {
		return err
	}
	assets, err := inventoryAssets(releaseDir)
	if err != nil {
		return err
	}
	manifest := releaseManifest{Schema: 1, Version: version, SourceSHA: sourceSHA, CIRunID: runID, CIRunAttempt: runAttempt, Qualification: q, Assets: assets}
	if err := writeJSON(output, manifest); err != nil {
		return err
	}
	digest, err := fileDigest(output)
	if err != nil {
		return err
	}
	return os.WriteFile(output+".sha256", []byte(digest+"  "+filepath.Base(output)+"\n"), 0o644)
}

func verifyRelease(manifestPath, releaseDir, repository, version, sourceSHA string, runID, runAttempt int64) error {
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid stable version %q", version)
	}
	var manifest releaseManifest
	if err := readJSON(manifestPath, &manifest); err != nil {
		return fmt.Errorf("read release manifest: %w", err)
	}
	if manifest.Schema != 1 || manifest.Version != version || manifest.SourceSHA != sourceSHA || manifest.CIRunID != runID || manifest.CIRunAttempt != runAttempt {
		return errors.New("release manifest does not identify the requested version, source, and CI run")
	}
	if err := verifyQualification(manifest.Qualification, repository, sourceSHA, runID, runAttempt); err != nil {
		return err
	}
	actual, err := inventoryAssets(releaseDir)
	if err != nil {
		return err
	}
	if !equalAssets(manifest.Assets, actual) {
		return errors.New("release directory names, sizes, or SHA-256 hashes do not exactly match the manifest")
	}
	checksum, err := os.ReadFile(manifestPath + ".sha256")
	if err != nil {
		return fmt.Errorf("read release manifest checksum: %w", err)
	}
	fields := strings.Fields(string(checksum))
	if len(fields) != 2 || fields[1] != filepath.Base(manifestPath) || !digestPattern.MatchString(fields[0]) {
		return errors.New("invalid release manifest checksum file")
	}
	digest, err := fileDigest(manifestPath)
	if err != nil {
		return err
	}
	if digest != fields[0] {
		return errors.New("release manifest checksum mismatch")
	}
	return nil
}

func inventoryAssets(dir string) ([]releaseAsset, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read release directory: %w", err)
	}
	var assets []releaseAsset
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "wago-") {
			continue
		}
		if !assetPattern.MatchString(name) {
			return nil, fmt.Errorf("unsafe release asset name %q", name)
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() < 1 {
			return nil, fmt.Errorf("release asset is not a non-empty regular file: %s", name)
		}
		digest, err := fileDigest(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		assets = append(assets, releaseAsset{Name: name, Size: info.Size(), SHA256: digest})
	}
	if len(assets) == 0 {
		return nil, errors.New("release directory has no wago-* assets")
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })
	return assets, nil
}

func equalAssets(a, b []releaseAsset) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] || !assetPattern.MatchString(a[i].Name) || a[i].Size < 1 || !digestPattern.MatchString(a[i].SHA256) {
			return false
		}
		if i > 0 && a[i-1].Name >= a[i].Name {
			return false
		}
	}
	return true
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func readJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON content: %w", err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".release-qualification-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(data.Bytes()); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(0o644); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
