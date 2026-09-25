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
	paths, err := filepath.Glob(filepath.Clean("../../../.github/workflows/*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yamlPaths, err := filepath.Glob(filepath.Clean("../../../.github/workflows/*.yaml"))
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

func TestCIUsesPinnedJustTaskRunner(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(workflow)
	for _, required := range []string{
		`JUST_VERSION: "1.58.0"`,
		`extractions/setup-just@53165ef7e734c5c07cb06b3c8e7b647c5aa16db3`,
		`run: just lint`,
		`run: just test unit`,
		`run: just test corpus all`,
		`run: just test spec v2`,
		`run: just build tinygo`,
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("CI workflow is missing just task-runner policy %q", required)
		}
	}
	if strings.Contains(contents, "run: make ") {
		t.Error("CI workflow still invokes the removed Makefile")
	}
}

func TestLongNativeCorpusRunsAreShardedAndVerified(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := workflowJobBlocks(string(workflow))
	shards, ok := jobs["corpus-correctness"]
	if !ok {
		t.Fatal("CI has no sharded native correctness-corpus job")
	}
	for _, required := range []string{
		"go run ./cmd/corpus-shard plan",
		"go run ./cmd/corpus-shard complete",
		"TestCorpus|TestCorpusSemanticExec",
		"corpus-correctness-${{ matrix.target }}-${{ matrix.shard }}",
		"darwin/amd64",
		"windows/arm64",
	} {
		if !strings.Contains(shards, required) {
			t.Errorf("native corpus shard job is missing %q", required)
		}
	}
	platform, ok := jobs["platform-test"]
	if !ok || !strings.Contains(platform, "matrix.runtime && matrix.corpus") {
		t.Fatal("fast native corpus lanes must remain in their platform jobs")
	}
	verify, ok := jobs["app-corpus-verify"]
	if !ok || !strings.Contains(verify, "needs: [changes, app-corpus, corpus-correctness]") || !strings.Contains(verify, "TestVerifyCorpusCorrectnessShardReports") {
		t.Fatal("corpus shard reports are not part of the coverage gate")
	}
	aggregate, ok := jobs["ci-ok"]
	if !ok || !strings.Contains(aggregate, "corpus-correctness") {
		t.Fatal("the CI aggregate does not require native corpus shards")
	}
}

func TestCanaryPublishesCommitAddressedArtifactsWithoutTags(t *testing.T) {
	canary, err := os.ReadFile(filepath.Clean("../../../.github/workflows/canary.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(canary)
	for _, required := range []string{
		`name: Publish canary artifacts`,
		`name: canary-${{ needs.prepare.outputs.sha }}-${{ matrix.target }}`,
		`WAGO_VERSION: canary@${{ needs.prepare.outputs.sha }}`,
		`scripts/smoke-release-assets.sh . "${{ matrix.target }}" "canary@${{ needs.prepare.outputs.sha }}"`,
		`scripts/release-qualification.sh verify-ci`,
		`retention-days: 90`,
		`group: publish-canary-${{ github.event.workflow_run.head_sha || inputs.source_sha || github.sha }}`,
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("canary workflow is missing commit-addressed artifact policy %q", required)
		}
	}
	for _, forbidden := range []string{"gh release", "/releases", "--prerelease", "git/ref/tags", "refs/tags/", "RELEASE_SERIES", "contents: write"} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("canary workflow must not publish tags or releases, found %q", forbidden)
		}
	}
}

func TestRegressionStressUsesReusableWorkflowProfiles(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/regression-stress.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := workflowJobBlocks(string(workflow))
	for job, required := range map[string]string{
		"gc-hardening":       "if: inputs.profile == 'full' || inputs.profile == 'release' || github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
		"stress":             "if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch' || inputs.profile == 'release'",
		"regression-rebuild": "if: inputs.profile == 'release'",
	} {
		block, ok := jobs[job]
		if !ok || !strings.Contains(block, required) {
			t.Errorf("reusable stress workflow job %s does not honor its caller profile", job)
		}
	}
}

func TestInstallerPublishStagesRemovedBootstraps(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/sync-install.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(workflow)
	if !strings.Contains(contents, "git add --all -- .") {
		t.Error("installer publish workflow must stage removed bootstrap files")
	}
	if strings.Contains(contents, "git add --all -- install.sh install.cmd install.ps1") {
		t.Error("installer publish workflow names the removed install.cmd path")
	}
	for _, required := range []string{"stable-source-qualification-", `scripts/release-qualification.sh verify-ci`, "github.event.workflow_run.conclusion == 'success'", "REQUESTED_SOURCE_SHA"} {
		if !strings.Contains(contents, required) {
			t.Errorf("installer publication is missing exact-source CI qualification %q", required)
		}
	}
}

func TestAggregateCIRequiresEveryWorkflowJob(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := workflowJobBlocks(string(workflow))
	aggregate, ok := jobs["ci-ok"]
	if !ok {
		t.Fatal("CI workflow has no ci-ok aggregate job")
	}
	needs := workflowJobNeeds(t, aggregate)
	for job := range jobs {
		if job == "ci-ok" {
			continue
		}
		if _, ok := needs[job]; !ok {
			t.Errorf("ci-ok aggregate does not depend on workflow job %q", job)
		}
	}
	for need := range needs {
		if _, ok := jobs[need]; !ok {
			t.Errorf("ci-ok aggregate depends on unknown workflow job %q", need)
		}
	}
	for _, required := range []string{
		"if: always()",
		"CI_NEEDS: ${{ toJSON(needs) }}",
		"go run ./tests/tools/ci-plan verify",
	} {
		if !strings.Contains(aggregate, required) {
			t.Errorf("ci-ok aggregate is missing fail-closed result handling %q", required)
		}
	}
}

func TestWebAssemblyV1ConformanceIsRequiredOnLinuxTargets(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := workflowJobBlocks(string(workflow))
	v1, ok := jobs["spec-v1"]
	if !ok {
		t.Fatal("CI has no dedicated WebAssembly 1.0 conformance job")
	}
	for _, required := range []string{
		"if: needs.changes.outputs.profile == 'full'",
		"name: Linux amd64",
		"name: Linux arm64",
		"git submodule update --init tests/conformance/spec-v1",
		"just test spec v1",
	} {
		if !strings.Contains(v1, required) {
			t.Errorf("WebAssembly 1.0 conformance job is missing %q", required)
		}
	}
	if !strings.Contains(jobs["ci-ok"], "spec-v1") {
		t.Fatal("the CI aggregate does not require WebAssembly 1.0 conformance")
	}
}

func TestCIDoesNotScheduleCoverage(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := workflowJobBlocks(string(workflow))
	if _, ok := jobs["coverage"]; ok {
		t.Fatal("CI must not schedule the coverage job for pull requests or main")
	}
}

func workflowJobBlocks(workflow string) map[string]string {
	lines := strings.Split(workflow, "\n")
	jobLine := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*(?:#.*)?$`)
	blocks := make(map[string]string)
	inJobs := false
	current := ""
	start := 0
	for i, line := range lines {
		if !inJobs {
			if line == "jobs:" {
				inJobs = true
			}
			continue
		}
		match := jobLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if current != "" {
			blocks[current] = strings.Join(lines[start:i], "\n")
		}
		current = match[1]
		start = i
	}
	if current != "" {
		blocks[current] = strings.Join(lines[start:], "\n")
	}
	return blocks
}

func workflowJobNeeds(t *testing.T, job string) map[string]struct{} {
	t.Helper()
	lines := strings.Split(job, "\n")
	for i, line := range lines {
		const prefix = "    needs:"
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			return splitWorkflowNeeds(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
		}
		if value != "" {
			return splitWorkflowNeeds(value)
		}
		needs := make(map[string]struct{})
		for _, item := range lines[i+1:] {
			if !strings.HasPrefix(item, "      - ") {
				break
			}
			name := strings.TrimSpace(strings.TrimPrefix(item, "      - "))
			if name != "" {
				needs[name] = struct{}{}
			}
		}
		return needs
	}
	t.Fatal("ci-ok aggregate job has no needs declaration")
	return nil
}

func splitWorkflowNeeds(value string) map[string]struct{} {
	needs := make(map[string]struct{})
	for _, item := range strings.Split(value, ",") {
		name := strings.TrimSpace(item)
		if name != "" {
			needs[name] = struct{}{}
		}
	}
	return needs
}

func TestReleasePublishesOnlyExactQualifiedArtifacts(t *testing.T) {
	releaseWorkflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	release := string(releaseWorkflow)
	if strings.Contains(release, "\n  push:") || strings.Contains(release, `tags: ["v*"]`) {
		t.Fatal("stable publication must not start automatically from a version tag")
	}
	for _, required := range []string{
		`workflow_dispatch:`,
		`source_sha:`,
		`release qualification must be dispatched from the main workflow`,
		`vMAJOR.MINOR.PATCH-beta.N or vMAJOR.MINOR.PATCH`,
		`installer module requires $VERSION`,
		`installer workspace replacement does not select $VERSION`,
		`actions: read`,
		`contents: write`,
		`Resolve successful CI run for the exact SHA`,
		`stable-source-qualification-${{ steps.inputs.outputs.source_sha }}-${{ steps.ci.outputs.run_attempt }}`,
		`scripts/release-qualification.sh verify-ci`,
		`Smoke-test the files that will be published`,
		`scripts/release-qualification.sh create-release`,
		`needs: [prepare, build, manifest]`,
		`ref: ${{ needs.prepare.outputs.source_sha }}`,
		`existing tag $VERSION does not point directly to qualified commit $SOURCE_SHA`,
		`INSTALLER_TAG: cli/wago-installer/${{ needs.prepare.outputs.version }}`,
		`refs/tags/$INSTALLER_TAG`,
		`existing installer module tag $INSTALLER_TAG does not point directly to qualified commit $SOURCE_SHA`,
		`published release $VERSION already exists; refusing to replace it`,
		`--json isDraft,isPrerelease,tagName`,
		`RESUME_DRAFT: ${{ steps.release.outputs.resume_draft }}`,
		`PRERELEASE: ${{ needs.prepare.outputs.prerelease }}`,
		`gh release upload "$VERSION" --repo "${{ github.repository }}"`,
		`release $VERSION became published before draft recovery`,
		`draft release assets do not exactly match the qualified manifest`,
		`releases/generate-notes`,
		`--verify-tag`,
		`--draft`,
		`--draft=false`,
		`--prerelease`,
		`release/release-manifest.json`,
	} {
		if !strings.Contains(release, required) {
			t.Errorf("stable release workflow is missing exact qualification policy %q", required)
		}
	}
	jobs := workflowJobBlocks(release)
	if !strings.Contains(jobs["build"], "scripts/build-release-assets.sh") {
		t.Fatal("stable build job does not create release assets")
	}
	if !strings.Contains(jobs["build"], "scripts/smoke-release-assets.sh") {
		t.Fatal("stable build job does not smoke-test release assets")
	}
	if strings.Contains(jobs["publish"], "scripts/build-release-assets.sh") {
		t.Fatal("stable publish job rebuilds release assets after qualification")
	}
	if !strings.Contains(jobs["publish"], `if [ "$RESUME_DRAFT" = "true" ]; then`) ||
		!strings.Contains(jobs["publish"], `--clobber "${assets[@]}"`) {
		t.Fatal("stable publication does not restrict asset replacement to a resumable draft")
	}
	for _, job := range []string{"prepare", "build", "manifest"} {
		if !strings.Contains(jobs[job], "overwrite: true") {
			t.Errorf("stable %s artifacts cannot be replaced safely by a workflow retry", job)
		}
	}

	ciWorkflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	ci := string(ciWorkflow)
	ciAggregate := workflowJobBlocks(ci)["ci-ok"]
	for _, required := range []string{
		`actions/checkout@11d5960a326750d5838078e36cf38b85af677262`,
		`ref: ${{ github.sha }}`,
		`Record exact main-branch qualification`,
		`CI_NEEDS: ${{ toJSON(needs) }}`,
		`stable-source-qualification-${{ github.sha }}-${{ github.run_attempt }}`,
		`scripts/release-qualification.sh record-ci stable-qualification/ci-qualification.json`,
	} {
		if !strings.Contains(ciAggregate, required) {
			t.Errorf("CI aggregate job is missing stable qualification record %q", required)
		}
	}
}

func TestWindowsWABTInstallIsPinnedAndVerified(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/ci.yml"))
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

	installer, err := os.ReadFile(filepath.Clean("../../scripts/install-wabt-windows.ps1"))
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

func TestRuntimeConcurrencyHarnessRunsOnLinuxAMD64AndARM64(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(workflow)
	for _, required := range []string{
		`name: Run bounded seeded runtime concurrency harness`,
		`WAGO_CONCURRENCY_SEED: 439000001,439000019,439000043,439000081`,
		`run: just test concurrency`,
		`name: Race detector / Linux amd64`,
		`timeout-minutes: 15`,
		`go test -race -count=1 ./src/wago ./src/core/runtime ./tests/integration/runtimeconcurrency`,
		`if: matrix.goos == 'linux'`,
	} {
		if strings.HasPrefix(required, "if:") {
			if !strings.Contains(contents, required) {
				t.Errorf("CI workflow is missing runtime-concurrency policy %q", required)
			}
		} else if !strings.Contains(contents, required) {
			t.Errorf("CI workflow is missing runtime-concurrency policy %q", required)
		}
	}
	if strings.Contains(contents, "runtime-concurrency:") {
		t.Error("seeded concurrency must be folded into the native test lane")
	}
}

func TestDocsChangesRunDocumentationValidation(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(workflow)
	for _, required := range []string{
		`ready_for_review`,
		`converted_to_draft`,
		`name: Plan CI profile`,
		`go run ./tests/tools/ci-plan plan`,
		`if: needs.changes.outputs.profile == 'docs' || needs.changes.outputs.profile == 'full'`,
		`run: just docs`,
		`name: Draft smoke / Linux amd64`,
		`CURRENT_GO_VERSION: "1.27.1"`,
		`name: Current Go / Linux amd64`,
		`run: just test ci-smoke`,
		`name: Verify every expected CI result`,
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("CI workflow is missing docs-validation policy %q", required)
		}
	}
}

func TestDocsOnlyChangesSkipCodeMatrix(t *testing.T) {
	workflow, err := os.ReadFile(filepath.Clean("../../../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(workflow)
	if strings.Contains(contents, "dorny/paths-filter") {
		t.Fatal("CI change selection must use the tested Go planner")
	}
	planner, err := os.ReadFile(filepath.Clean("../../tools/ci-plan/main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"documentationPath", "return planFor(\"docs\"", "unexpected pull request base", "change detection returned an empty selection"} {
		if !strings.Contains(string(planner), required) {
			t.Errorf("CI planner is missing fail-closed path policy %q", required)
		}
	}
}
