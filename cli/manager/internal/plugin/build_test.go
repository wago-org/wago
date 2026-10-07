package plugin

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
	"github.com/wago-org/wago/internal/filelock"
	"github.com/wago-org/wago/internal/wagopaths"
)

func init() {
	if os.Getenv("WAGO_TEST_GRANT_CONTEXT") == "1" {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		Grant(MutationRequest{Context: ctx, Local: true, Name: "ignored"})
		os.Exit(2)
	}
	switch os.Getenv("WAGO_TEST_STAGED_RUNTIME") {
	case "output":
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), 256<<10))
		os.Exit(0)
	case "clean":
		os.Exit(0)
	case "slow":
		time.Sleep(11 * time.Second)
		os.Exit(0)
	case "tree-parent", "tree-parent-exit":
		mode := os.Getenv("WAGO_TEST_STAGED_RUNTIME")
		_ = os.WriteFile(os.Getenv("WAGO_TEST_TREE_READY"), []byte("ready"), 0o600)
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "WAGO_TEST_STAGED_RUNTIME=tree-child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(2)
		}
		if mode == "tree-parent" {
			time.Sleep(3 * time.Second)
		}
		os.Exit(0)
	case "tree-child":
		delay, err := time.ParseDuration(os.Getenv("WAGO_TEST_TREE_DELAY"))
		if err != nil {
			delay = 11 * time.Second
		}
		time.Sleep(delay)
		_ = os.WriteFile(os.Getenv("WAGO_TEST_TREE_SURVIVED"), []byte("survived"), 0o600)
		os.Exit(0)
	}
}

func TestParsePluginSpecExpandsGitHubShorthand(t *testing.T) {
	id, constraint, err := parsePluginSpec("wago-org/wasi@^1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if id != "github.com/wago-org/wasi" || constraint != "^1.2.3" {
		t.Fatalf("parsePluginSpec shorthand = %q, %q; want github.com/wago-org/wasi, ^1.2.3", id, constraint)
	}
}

func TestPluginRuntimeBinaryResolvesGlobalBuild(t *testing.T) {
	buildDir, _ := prepareTestPluginRuntime(t)
	got, configured, err := pluginRuntimeBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !configured || got != pluginbuild.BinaryPath(buildDir) {
		t.Fatalf("plugin runtime = %q, %v; want %q, true", got, configured, pluginbuild.BinaryPath(buildDir))
	}
}

func TestPublishedStagedRuntimeKeepsItsVerification(t *testing.T) {
	buildDir, manifestDir := prepareTestPluginRuntime(t)
	manifest := map[string]any{"$schema": project.SchemaURI, "plugins": map[string]any{}}
	lock := project.NewLockDocument()
	config := pluginbuild.Config{RuntimeVersion: "test", Profile: "standard"}
	ctx := context.Background()
	if err := project.WithMutation(ctx, manifestDir, func(mutation *project.Mutation) error {
		return stageAndPublishLockedState(ctx, mutation, manifestDir, buildDir, manifest, lock, false, config)
	}); err != nil {
		t.Fatal(err)
	}
	verifications := 0
	_, cached, err := pluginbuild.EnsureVerifiedBinaryContext(ctx, buildDir, pluginbuild.Input{}, false, false, config, func(context.Context, string) error {
		verifications++
		return nil
	})
	if err != nil || !cached || verifications != 0 {
		t.Fatalf("first selection after publication: cached %v, verifications %d, err %v", cached, verifications, err)
	}
}

func TestPluginRuntimeBinaryBlocksConcurrentPublication(t *testing.T) {
	buildDir, manifestDir := prepareTestPluginRuntime(t)
	buildLock := buildDir + ".lock"
	if err := os.Mkdir(buildLock, 0o755); err != nil {
		t.Fatal(err)
	}
	buildLockReleased := false
	releaseBuildLock := func() {
		if !buildLockReleased {
			buildLockReleased = true
			if err := os.Remove(buildLock); err != nil && !os.IsNotExist(err) {
				t.Errorf("release test build lock: %v", err)
			}
		}
	}
	t.Cleanup(releaseBuildLock)

	runtimeDone := make(chan error, 1)
	go func() {
		_, _, err := pluginRuntimeBinary()
		runtimeDone <- err
	}()
	waitForPluginBuildLock(t)

	projectLockPath := filepath.Join(manifestDir, ".wago", "project.lock")
	probe, err := filelock.TryAcquireExisting(projectLockPath)
	if err != nil {
		t.Fatal(err)
	}
	runtimeOwnsProjectLock := probe == nil
	if probe != nil {
		if err := probe.Close(); err != nil {
			t.Fatal(err)
		}
	}

	publisherStarted := make(chan struct{})
	publisherEntered := make(chan struct{})
	publisherDone := make(chan error, 1)
	go func() {
		close(publisherStarted)
		publisherDone <- project.WithMutation(context.Background(), manifestDir, func(*project.Mutation) error {
			close(publisherEntered)
			return nil
		})
	}()
	<-publisherStarted
	if !runtimeOwnsProjectLock {
		select {
		case <-publisherEntered:
		case <-time.After(5 * time.Second):
			t.Fatal("publisher did not enter after runtime released the project lock")
		}
	}
	releaseBuildLock()
	if err := receivePluginTestResult(t, runtimeDone, "plugin runtime"); err != nil {
		t.Fatal(err)
	}
	if err := receivePluginTestResult(t, publisherDone, "plugin publication"); err != nil {
		t.Fatal(err)
	}
	if !runtimeOwnsProjectLock {
		t.Fatal("plugin metadata publication overlapped active runtime reconciliation")
	}
}

func waitForPluginBuildLock(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	stacks := make([]byte, 1<<20)
	for time.Now().Before(deadline) {
		length := runtime.Stack(stacks, true)
		if bytes.Contains(stacks[:length], []byte("plugin/build.acquireBuildLock")) {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("plugin runtime did not block on the active build lock")
}

func receivePluginTestResult(t *testing.T, result <-chan error, operation string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	// Windows CI may need tens of seconds to rebuild the generated executable
	// after the test releases its lock; the timeout guards deadlocks, not speed.
	case <-time.After(time.Minute):
		t.Fatalf("%s did not finish", operation)
		return nil
	}
}

func prepareTestPluginRuntime(t *testing.T) (buildDir, manifestDir string) {
	t.Helper()
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "")
	t.Setenv("WAGO_GLOBAL", "")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	emptyProject := t.TempDir()
	if err := os.Chdir(emptyProject); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	source := t.TempDir()
	for path, body := range map[string]string{
		"go.mod": "module github.com/wago-org/wago\n\ngo 1.22\n",
		"wago.go": `package wago
type PluginSelection struct{}
type PluginProvider struct{}
type PluginSet struct { Providers []PluginProvider; Selections []PluginSelection }
func InspectPluginPlan(PluginSet) (any, error) { return nil, nil }
`,
		"cli/runtime/runtime.go": `package runtime
import wago "github.com/wago-org/wago"
func MainWithPluginSet(string, string, wago.PluginSet) {}
`,
		"register/register.go": `package register
import wago "github.com/wago-org/wago"
func Providers() []wago.PluginProvider { return nil }
`,
	} {
		fullPath := filepath.Join(source, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WAGO_SRC", source)

	buildDir, err = buildDirFor(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pluginbuild.EnsureModule(buildDir); err != nil {
		t.Fatal(err)
	}
	manifestDir = sharedGlobalPluginDir(wago.DirsFor(managerVersion()))
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const plugin = "github.com/wago-org/wago"
	if _, err := project.AddDependency(manifestDir, plugin, "^0.0.0"); err != nil {
		t.Fatal(err)
	}

	lock := project.NewLockDocument()
	entry := testManagerLockEntry(plugin)
	lock.Plugins[plugin] = entry
	if err := project.WriteLock(manifestDir, lock); err != nil {
		t.Fatal(err)
	}
	return buildDir, manifestDir
}

func TestLockedPluginResolutionRequiresPinnedVersionsBeforeBuilding(t *testing.T) {
	automation.Reset()
	t.Cleanup(automation.Reset)
	manifestDir := t.TempDir()
	if _, err := project.AddDependency(manifestDir, "github.com/wago-org/wasi", "^1.0.0"); err != nil {
		t.Fatal(err)
	}
	buildDir := filepath.Join(t.TempDir(), "not-created")
	t.Setenv(automation.EnvLocked, "1")

	_, err := syncLockedPluginVersions(buildDir, manifestDir, false)
	if err == nil || !strings.Contains(err.Error(), "wago-org/wasi") {
		t.Fatalf("locked resolution error = %v", err)
	}
	if _, err := os.Stat(buildDir); !os.IsNotExist(err) {
		t.Fatalf("locked resolution touched build state: %v", err)
	}
}

func TestVerifySourceChecksumsReconcilesGeneratedModule(t *testing.T) {
	buildDir := t.TempDir()
	// Go 1.26 normalizes a two-component go directive to three components and
	// otherwise rejects `go list` before it can report the selected modules.
	// Generated plugin modules must be reconciled before checksum verification.
	if err := os.WriteFile(filepath.Join(buildDir, "go.mod"), []byte("module wago.local/build\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifySourceChecksums(buildDir, nil); err != nil {
		t.Fatalf("verifySourceChecksums: %v", err)
	}
}

func TestVerifyStagedRuntimeBoundsUntrustedProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("clean runtime", func(t *testing.T) {
		t.Setenv("WAGO_TEST_STAGED_RUNTIME", "clean")
		if err := verifyStagedRuntime(executable); err != nil {
			t.Fatalf("contained runtime failed validation: %v", err)
		}
	})
	t.Run("output", func(t *testing.T) {
		t.Setenv("WAGO_TEST_STAGED_RUNTIME", "output")
		err := verifyStagedRuntime(executable)
		if err == nil || !strings.Contains(err.Error(), "output limit") {
			length := 0
			if err != nil {
				length = len(err.Error())
			}
			t.Fatalf("expected bounded-output error; non-nil=%t length=%d", err != nil, length)
		}
		if len(err.Error()) > 70<<10 {
			t.Fatalf("verification error retained unbounded process output: %d bytes", len(err.Error()))
		}
	})
	t.Run("process tree", func(t *testing.T) {
		dir := t.TempDir()
		ready := filepath.Join(dir, "ready")
		survived := filepath.Join(dir, "survived")
		t.Setenv("WAGO_TEST_STAGED_RUNTIME", "tree-parent")
		t.Setenv("WAGO_TEST_TREE_READY", ready)
		t.Setenv("WAGO_TEST_TREE_SURVIVED", survived)
		t.Setenv("WAGO_TEST_TREE_DELAY", "2s")
		started := time.Now()
		err := verifyStagedRuntime(executable)
		if err == nil {
			t.Fatalf("runtime verification returned after %s with %v", time.Since(started), err)
		}
		if runtime.GOOS != "windows" {
			// Windows validation uses a write-restricted token. The process's
			// exit status above proves child creation was denied without relying
			// on marker writes that the token intentionally cannot perform.
			if _, err := os.Stat(ready); err != nil {
				t.Fatalf("validation descendant did not start: %v", err)
			}
			if wait := time.Until(started.Add(3 * time.Second)); wait > 0 {
				time.Sleep(wait)
			}
			if _, err := os.Stat(survived); !os.IsNotExist(err) {
				t.Fatalf("validation descendant survived cancellation: %v", err)
			}
		}
	})
	t.Run("early parent exit", func(t *testing.T) {
		dir := t.TempDir()
		ready := filepath.Join(dir, "ready")
		survived := filepath.Join(dir, "survived")
		t.Setenv("WAGO_TEST_STAGED_RUNTIME", "tree-parent-exit")
		t.Setenv("WAGO_TEST_TREE_READY", ready)
		t.Setenv("WAGO_TEST_TREE_SURVIVED", survived)
		t.Setenv("WAGO_TEST_TREE_DELAY", "2s")
		started := time.Now()
		if err := verifyStagedRuntime(executable); err == nil {
			t.Fatal("verification accepted a runtime that left a background child")
		}
		if runtime.GOOS != "windows" {
			if _, err := os.Stat(ready); err != nil {
				t.Fatalf("validation descendant did not start: %v", err)
			}
			if wait := time.Until(started.Add(2500 * time.Millisecond)); wait > 0 {
				time.Sleep(wait)
			}
			if _, err := os.Stat(survived); !os.IsNotExist(err) {
				t.Fatalf("validation descendant survived parent exit: %v", err)
			}
		}
	})
	t.Run("deadline", func(t *testing.T) {
		t.Setenv("WAGO_TEST_STAGED_RUNTIME", "slow")
		started := time.Now()
		err := verifyStagedRuntime(executable)
		if elapsed := time.Since(started); err == nil || !strings.Contains(err.Error(), "deadline exceeded") || elapsed >= 11*time.Second {
			t.Fatalf("runtime verification returned after %s with %v", elapsed, err)
		}
	})
}

func TestVerifyStagedRuntimeStartFailureReturnsError(t *testing.T) {
	err := verifyStagedRuntime(filepath.Join(t.TempDir(), "missing-runtime"))
	if err == nil || !strings.Contains(err.Error(), "verify staged plugin runtime") {
		t.Fatalf("missing staged runtime error = %v", err)
	}
}

func TestLinuxStagedRuntimePinsSeccompThreadUntilExec(t *testing.T) {
	// Seccomp is thread-local. Keep this source-level guard because a scheduler
	// migration in the tiny interval before Exec is not deterministic in a test.
	body, err := os.ReadFile("staged_runtime_command_linux.go")
	if err != nil {
		t.Fatal(err)
	}
	lock := bytes.Index(body, []byte("runtime.LockOSThread()"))
	filter := bytes.Index(body, []byte("prohibitStagedRuntimeProcesses()"))
	exec := bytes.Index(body, []byte("syscall.Exec("))
	if lock < 0 || filter < 0 || exec < 0 || !(lock < filter && filter < exec) {
		t.Fatal("Linux helper must pin its OS thread before installing seccomp and keep it pinned through Exec")
	}
}

func TestGrantPropagatesMutationContext(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable)
	command.Dir = t.TempDir()
	command.Env = append(os.Environ(), "WAGO_TEST_GRANT_CONTEXT=1", "WAGO_HOME="+t.TempDir())
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), context.Canceled.Error()) {
		t.Fatalf("Grant with canceled request context returned %v:\n%s", err, output)
	}
}

func TestLockedPluginResolutionRejectsPreexistingSourceReplace(t *testing.T) {
	const plugin = "github.com/acme/plugin"
	manifestDir := t.TempDir()
	if _, err := project.AddDependency(manifestDir, plugin, "^1.0.0"); err != nil {
		t.Fatal(err)
	}
	lock := project.NewLockDocument()
	entry := testManagerLockEntry(plugin)
	entry.Source.Version = "v1.0.0"
	lock.Plugins[plugin] = entry
	if err := project.WriteLock(manifestDir, lock); err != nil {
		t.Fatal(err)
	}

	buildDir := t.TempDir()
	local := filepath.Join(buildDir, "local-plugin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "go.mod"), []byte("module "+plugin+"\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	goMod := "module wago.local/build\n\ngo 1.22\n\nrequire " + plugin + " v1.0.0\n\nreplace " + plugin + " => ./local-plugin\n"
	if err := os.WriteFile(filepath.Join(buildDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := syncLockedPluginVersions(buildDir, manifestDir, false)
	if err == nil || !strings.Contains(err.Error(), "locked plugin source "+plugin+"@v1.0.0") || !strings.Contains(err.Error(), "go.mod replace") {
		t.Fatalf("locked replacement = changed %v, err %v", changed, err)
	}
	body, readErr := os.ReadFile(filepath.Join(buildDir, "go.mod"))
	if readErr != nil || !strings.Contains(string(body), "replace "+plugin+" => ./local-plugin") {
		t.Fatalf("rejected replacement was silently reconciled = %q, %v", body, readErr)
	}
}

type testEnvironment struct{}

func (testEnvironment) SelectScope(global, local, bare bool) error {
	return Select(global, local, bare)
}

func (testEnvironment) RuntimeBinary() (string, bool, error) {
	return RuntimeBinary()
}

func TestRuntimePathForInvocationLeavesMinimalRuntimeAlone(t *testing.T) {
	const base = "/runtime/wago"
	got, err := Resolve(base, wagopaths.ProfileMinimal, []string{"run", "module.wasm"}, testEnvironment{})
	if err != nil {
		t.Fatal(err)
	}
	if got != base {
		t.Fatalf("minimal runtime path = %q, want %q", got, base)
	}
}

func testManagerLockEntry(id string) project.LockEntry {
	return project.LockEntry{
		Direct: true, Source: project.PluginSource{Module: id, Version: "v0.0.0", Checksum: "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
		Provider:           project.ProviderSource{ImportPath: id + "/register"},
		DefinitionDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ReleaseFingerprint: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Dependencies:       map[string]string{}, RequestedAuthorities: []project.AuthorityRequest{}, Grants: []project.AuthorityGrant{},
		Contracts: project.ContractSet{Provides: []project.ContractProvider{}, Requires: []project.ContractRequirement{}},
		Bindings:  []project.ContractBinding{}, Config: []byte(`{}`),
	}
}
