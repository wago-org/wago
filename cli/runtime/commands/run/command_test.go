package run

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/settings"
	"github.com/wago-org/wago/cli/runtime/internal/artifactcache"
)

type testEnvironment struct{}

func (testEnvironment) ProfileFlags() []command.Flag {
	return []command.Flag{{Name: "local", Bool: true}, {Name: "global", Short: "g", Bool: true}}
}
func (testEnvironment) LoadRuntime(cfg *wago.RuntimeConfig, guestArgs []string) *wago.Runtime {
	return wago.NewRuntime(wago.WithRuntimeConfig(cfg), wago.WithGuestArguments(guestArgs))
}
func (testEnvironment) ArtifactCache() artifactcache.Cache { return artifactcache.Cache{} }

type runTeardownEnvironment struct{ plugin *runTeardownPlugin }

func (runTeardownEnvironment) ProfileFlags() []command.Flag { return nil }

func (environment runTeardownEnvironment) LoadRuntime(config *wago.RuntimeConfig, guestArgs []string) *wago.Runtime {
	runtime := wago.NewRuntime(wago.WithRuntimeConfig(config), wago.WithGuestArguments(guestArgs))
	definition := wago.PluginDefinition{
		ID: "example.com/cli/run-teardown", Version: "1.0.0",
		Provenance: wago.PluginProvenance{Repository: "https://example.com/cli/run-teardown", License: "MIT"},
		Authorities: []wago.AuthorityRequest{{
			Name: wago.AuthorityModuleCloseObserve, Mode: wago.AuthorityRequired, Reason: "verify run teardown",
		}},
	}
	grants := []wago.AuthorityGrant{{Name: wago.AuthorityModuleCloseObserve}}
	if environment.plugin.exitCode != nil {
		definition.Authorities = append(definition.Authorities, wago.AuthorityRequest{
			Name: wago.AuthorityHostImportDefine, Mode: wago.AuthorityRequired,
			Reason: "request a guest exit", Scope: wago.AuthorityScope{Modules: []string{"env"}},
		})
		grants = append(grants, wago.AuthorityGrant{
			Name: wago.AuthorityHostImportDefine, Scope: wago.AuthorityScope{Modules: []string{"env"}},
		})
	}
	digest, err := wago.DefinitionDigest(definition)
	if err != nil {
		panic(err)
	}
	plugins := wago.PluginSet{
		Providers: []wago.PluginProvider{{Definition: definition, New: func() wago.Plugin { return environment.plugin }}},
		Selections: []wago.PluginSelection{{
			ID: definition.ID, DefinitionDigest: digest, Direct: true,
			Dependencies: map[string]string{},
			Grants:       grants,
		}},
	}
	if err := runtime.LoadPlugins(context.Background(), plugins); err != nil {
		panic(err)
	}
	return runtime
}

func (runTeardownEnvironment) ArtifactCache() artifactcache.Cache { return artifactcache.Cache{} }

type runTeardownPlugin struct {
	moduleClosed func()
	stop         func(context.Context) error
	exitCode     *int32
}

func (plugin *runTeardownPlugin) Register(registrar *wago.Registrar) error {
	if plugin.exitCode != nil {
		imports, err := registrar.HostImports()
		if err != nil {
			return err
		}
		imports.HostFunc("env", "exit", func() { panic(wago.HostExit{Code: *plugin.exitCode}) })
	}
	observer, err := registrar.ModuleCloseObserver()
	if err != nil {
		return err
	}
	if err := observer.Observe(func(wago.ModuleCloseEvent) { plugin.moduleClosed() }); err != nil {
		return err
	}
	return registrar.Lifecycle(wago.PluginLifecycle{Stop: plugin.stop})
}

func TestOptimizationFlags(t *testing.T) {
	knobs := wago.NewRuntimeConfig().OptimizationInfos()
	if len(knobs) == 0 {
		t.Fatal("no optimization knobs")
	}
	flags := OptimizationFlags()
	if len(flags) != len(knobs)*2 {
		t.Fatalf("optimization flags = %d, want %d", len(flags), len(knobs)*2)
	}
	for index, knob := range knobs {
		if flags[index*2].Name != knob.Name || !flags[index*2].Bool ||
			flags[index*2+1].Name != "no-"+knob.Name || !flags[index*2+1].Bool {
			t.Fatalf("flag pair %d = %#v, %#v", index, flags[index*2], flags[index*2+1])
		}
	}
	name := knobs[0].Name
	enabled, err := OptimizationOverrides(command.NewContext(nil, nil, map[string]bool{name: true}))
	if err != nil || !enabled[name] {
		t.Fatalf("--%s override = %v, %v", name, enabled, err)
	}
	disabled, err := OptimizationOverrides(command.NewContext(nil, nil, map[string]bool{"no-" + name: true}))
	if err != nil || disabled[name] {
		t.Fatalf("--no-%s override = %v, %v", name, disabled, err)
	}
}

func TestSettingsCatalogMatchesActiveBackendKnobs(t *testing.T) {
	backend := map[string]bool{}
	for _, knob := range wago.NewRuntimeConfig().OptimizationInfos() {
		backend[knob.Name] = true
	}
	catalog := settings.OptimizationsForArch(runtime.GOARCH)
	if len(catalog) != len(backend) {
		t.Fatalf("%s settings knobs = %d, backend knobs = %d", runtime.GOARCH, len(catalog), len(backend))
	}
	for _, knob := range catalog {
		name := strings.TrimPrefix(knob.Key, "optimizations.")
		if !backend[name] {
			t.Errorf("settings catalog knob %q is missing from %s backend", name, runtime.GOARCH)
		}
	}
}

func TestHelpCollapsesBooleanPairs(t *testing.T) {
	cmd := Command(testEnvironment{})
	var output strings.Builder
	cmd.PrintHelp(&output, "wago run")
	text := output.String()
	if strings.Contains(text, "--<no->st-flags") || !strings.Contains(text, "--help-optimizations") {
		t.Fatalf("run help did not collapse advanced optimization help:\n%s", text)
	}
	if !strings.Contains(text, "--parallel, -p [workers]") ||
		!strings.Contains(text, "--native-stack <size>") ||
		!strings.Contains(text, "--gc-heap <size>") ||
		!strings.Contains(text, "--gc-nursery <size>") ||
		!strings.Contains(text, "-p8 / -p 8 / --parallel=8") ||
		!strings.Contains(text, "use -- before colliding guest flags") {
		t.Fatalf("run help did not document function parallelism:\n%s", text)
	}
	output.Reset()
	cmd.PrintOptimizationHelp(&output, "wago run")
	text = output.String()
	if !strings.Contains(text, "--<no->st-flags") || strings.Contains(text, "enable: keep comparison results") {
		t.Fatalf("optimization help did not collapse boolean pair:\n%s", text)
	}
	deferredIndex := strings.Index(text, "--<no->deferred-bounds-checking")
	optimizationFlags := OptimizationFlags()
	lastKnobIndex := strings.Index(text, "--<no->"+optimizationFlags[len(optimizationFlags)-2].Name)
	if deferredIndex < 0 || lastKnobIndex < deferredIndex {
		t.Fatalf("optimization knobs are not ordered in advanced help:\n%s", text)
	}
}

func TestHelpRecognitionAfterSeparatedParallelism(t *testing.T) {
	cmd := Command(testEnvironment{})
	normalized, err := cmd.Normalize([]string{"-p", "8", "--help"})
	if err != nil || !command.WantsHelp(normalized, cmd.PassThrough, cmd.Flags) {
		t.Fatalf("help after separated parallelism was missed: normalized=%v err=%v", normalized, err)
	}
}

func TestRunRecognizesFlagsAfterModulePath(t *testing.T) {
	cmd := Command(testEnvironment{})
	args, err := cmd.Normalize([]string{"module.wasm", "--global"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := cmd.Parse("wago run", args)
	if err != nil {
		t.Fatal(err)
	}
	if !ctx.Bool("global") || len(ctx.Args) != 1 || ctx.Args[0] != "module.wasm" {
		t.Fatalf("file --global parsed as global=%v args=%v", ctx.Bool("global"), ctx.Args)
	}
}

func TestRunNativeStackFlag(t *testing.T) {
	cmd := Command(testEnvironment{})
	for _, tc := range []struct {
		value string
		want  uint64
	}{
		{"512KiB", 512 << 10},
		{"8MiB", 8 << 20},
		{"1GiB", 1 << 30},
		{"8388608B", 8 << 20},
		{"8388608", 8 << 20},
	} {
		normalized, err := cmd.Normalize([]string{"module.wasm", "--native-stack", tc.value})
		if err != nil {
			t.Fatalf("normalize %q: %v", tc.value, err)
		}
		ctx, err := cmd.Parse("wago run", normalized)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.value, err)
		}
		got, err := parseNativeStackBytes(ctx.Str("native-stack"))
		if err != nil || got != tc.want {
			t.Fatalf("native stack %q = %d, %v; want %d", tc.value, got, err, tc.want)
		}
		if len(ctx.Args) != 1 || ctx.Args[0] != "module.wasm" {
			t.Fatalf("native stack flag consumed guest arguments: %v", ctx.Args)
		}
	}
	for _, value := range []string{"", "511KiB", "524289", "2GiB", "1GB", "many"} {
		if _, err := parseNativeStackBytes(value); err == nil {
			t.Errorf("native stack %q was accepted", value)
		}
	}
}

func TestRunGCHeapFlags(t *testing.T) {
	cmd := Command(testEnvironment{})
	normalized, err := cmd.Normalize([]string{"module.wasm", "--gc-heap", "2GiB", "--gc-nursery=64MiB"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := cmd.Parse("wago run", normalized)
	if err != nil {
		t.Fatal(err)
	}
	cfg, configured, err := gcConfiguration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !configured || cfg.ThroughputHeapBytes != 2<<30 || cfg.NurseryBytes != 64<<20 {
		t.Fatalf("GC config = %+v, configured=%v", cfg, configured)
	}
	if len(ctx.Args) != 1 || ctx.Args[0] != "module.wasm" {
		t.Fatalf("GC flags consumed guest arguments: %v", ctx.Args)
	}

	for _, value := range []string{"0", "4GiB", "1GB", "many"} {
		ctx := command.NewContext(nil, map[string]string{"gc-heap": value}, nil)
		if _, _, err := gcConfiguration(ctx); err == nil {
			t.Errorf("gc heap %q was accepted", value)
		}
	}
	if _, configured, err := gcConfiguration(command.NewContext(nil, nil, nil)); err != nil || configured {
		t.Fatalf("default GC config = configured %v, error %v", configured, err)
	}
}

func TestRunParsesNativeArtifactOptInAsBoolean(t *testing.T) {
	cmd := Command(testEnvironment{})
	for _, args := range [][]string{
		{"--allow-native-artifact", "module.wago"},
		{"module.wago", "--allow-native-artifact"},
	} {
		normalized, err := cmd.Normalize(args)
		if err != nil {
			t.Fatal(err)
		}
		ctx, err := cmd.Parse("wago run", normalized)
		if err != nil {
			t.Fatalf("parse %v: %v", args, err)
		}
		if !ctx.Bool("allow-native-artifact") || len(ctx.Args) != 1 || ctx.Args[0] != "module.wago" {
			t.Fatalf("parse %v = opt-in %v, args %v", args, ctx.Bool("allow-native-artifact"), ctx.Args)
		}
	}
}

func TestFriendlyInstantiationErrorIsProviderNeutral(t *testing.T) {
	for _, importName := range []string{"wasi_snapshot_preview1.fd_write", "acme_host.render"} {
		err := fmt.Errorf(`module imports %q, but nothing provides it: %w`, importName, wago.ErrMissingImport)
		got := friendlyInstantiationError(err).Error()
		if !strings.Contains(got, importName) ||
			!strings.Contains(got, "Add a plugin that provides it") {
			t.Fatalf("missing import error = %q", got)
		}
		if strings.Contains(strings.ToLower(got), "wasi support") || strings.Contains(got, "wago-org/wasi") {
			t.Fatalf("missing import error endorses a provider: %q", got)
		}
	}

	other := errors.New("compile failed")
	if got := friendlyInstantiationError(other); !errors.Is(got, other) {
		t.Fatalf("unrelated error = %v, want original", got)
	}
}

func TestTrapReasonIncludesWasmFrame(t *testing.T) {
	got := trapReason(&wago.TrapError{
		Code: wago.TrapUnreachable,
		Frames: []wago.TrapFrame{{
			FunctionIndex: 2, FunctionName: "boom", ProgramCounter: 7, HasProgramCounter: true,
		}},
	})
	if !strings.Contains(got, "unreachable instruction executed") ||
		!strings.Contains(got, "at boom (func[2], wasm pc 0x7)") {
		t.Fatalf("trap reason = %q", got)
	}
}

func TestLoadModuleAndResolveExport(t *testing.T) {
	// (module (func (export "f") (result i32) i32.const 7))
	wasm := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0,
		7, 5, 1, 1, 'f', 0, 0,
		10, 6, 1, 4, 0, 0x41, 7, 0x0b}
	path := filepath.Join(t.TempDir(), "f.wasm")
	if err := os.WriteFile(path, wasm, 0o600); err != nil {
		t.Fatal(err)
	}
	rt := wago.NewRuntime()
	defer rt.Close()
	config := wago.NewRuntimeConfig()
	mod := mustLoadModule(path, config, rt, artifactcache.Cache{}, false)
	if got := mustResolveExport(mod.Compiled(), ""); got != "f" {
		t.Fatalf("default export = %q", got)
	}
	if got := mustResolveExport(mod.Compiled(), "f"); got != "f" {
		t.Fatalf("named export = %q", got)
	}
	encoded, err := mod.Compiled().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	compiledPath := filepath.Join(t.TempDir(), "f.wago")
	if err := os.WriteFile(compiledPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if mod, err := loadModule(compiledPath, config, rt, artifactcache.Cache{}, false); err == nil || mod != nil || !strings.Contains(err.Error(), "--allow-native-artifact") {
		t.Fatalf("untrusted artifact load = %v, %v; want explicit opt-in", mod, err)
	}
	if got := mustResolveExport(mustLoadModule(compiledPath, config, rt, artifactcache.Cache{}, true).Compiled(), "f"); got != "f" {
		t.Fatalf("loaded export = %q", got)
	}
	withTrailing := append(append([]byte(nil), encoded...), 0)
	if compiled, err := loadCompiledArtifact(withTrailing); err == nil || compiled != nil || !strings.Contains(err.Error(), "trailing 1 byte") {
		t.Fatalf("artifact with trailing byte = %v, %v; want rejection", compiled, err)
	}
	if compiled, err := loadCompiledArtifactReader(bytes.NewReader(withTrailing), -1); err == nil || compiled != nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("streamed artifact with trailing byte = %v, %v; want rejection", compiled, err)
	}
}

func TestLoadCompiledArtifactEnforcesSectionLimits(t *testing.T) {
	// Derive the header from the current encoder so format revisions do not
	// turn section-limit checks into version-mismatch checks.
	rt := wago.NewRuntime()
	defer rt.Close()
	module, err := rt.Compile([]byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := module.Compiled().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	limits := wago.DefaultArtifactLimits()
	for _, tc := range []struct {
		name        string
		codeBytes   uint64
		metadataLen uint64
		want        string
	}{
		{name: "code", codeBytes: uint64(limits.MaxCodeBytes) + 1, want: "code section length"},
		{name: "metadata", metadataLen: uint64(limits.MaxMetadataBytes) + 1, want: "metadata section length"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artifact := append([]byte(nil), encoded[:7]...)
			artifact = binary.AppendUvarint(artifact, tc.codeBytes)
			if tc.codeBytes == 0 {
				artifact = append(artifact, 2)
				artifact = binary.AppendUvarint(artifact, tc.metadataLen)
			}
			compiled, err := loadCompiledArtifact(artifact)
			if err == nil || compiled != nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "exceeds limit") {
				t.Fatalf("loadCompiledArtifact = %v, %v; want bounded %s error", compiled, err, tc.want)
			}
		})
	}
}

func TestRunParallelFlagForms(t *testing.T) {
	cmd := Command(testEnvironment{})
	for _, tc := range []struct {
		name           string
		args           []string
		wantParallel   string
		wantInvoke     string
		wantNoDeferred bool
		wantCore       string
	}{
		{name: "bare short", args: []string{"-p", "module.wasm"}, wantParallel: "auto"},
		{name: "joined short", args: []string{"-p8", "module.wasm"}, wantParallel: "8"},
		{name: "separated short", args: []string{"-p", "8", "module.wasm"}, wantParallel: "8"},
		{name: "equal short", args: []string{"-p=8", "module.wasm"}, wantParallel: "8"},
		{name: "bare long", args: []string{"--parallel", "module.wasm"}, wantParallel: "auto"},
		{name: "equal long", args: []string{"--parallel=8", "module.wasm"}, wantParallel: "8"},
		{name: "after separated invoke", args: []string{"-e", "add", "-p8", "module.wasm"}, wantParallel: "8", wantInvoke: "add"},
		{name: "after bounds knob", args: []string{"--no-deferred-bounds-checking", "-p", "module.wasm"}, wantParallel: "auto", wantNoDeferred: true},
		{name: "after separated core", args: []string{"--core", "3", "-p", "module.wasm"}, wantParallel: "auto", wantCore: "3"},
		{name: "parallel-looking invoke value", args: []string{"-e", "-p8", "module.wasm"}, wantInvoke: "-p8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := cmd.Normalize(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			ctx, err := cmd.Parse("wago run", args)
			if err != nil {
				t.Fatal(err)
			}
			if got := ctx.Str("parallel"); got != tc.wantParallel {
				t.Fatalf("parallel = %q, want %q (normalized %v)", got, tc.wantParallel, args)
			}
			if got := ctx.Str("invoke"); got != tc.wantInvoke {
				t.Fatalf("invoke = %q, want %q (normalized %v)", got, tc.wantInvoke, args)
			}
			if got := ctx.Bool("no-deferred-bounds-checking"); got != tc.wantNoDeferred {
				t.Fatalf("no deferred bounds checking = %v, want %v (normalized %v)", got, tc.wantNoDeferred, args)
			}
			if got := ctx.Str("core"); got != tc.wantCore {
				t.Fatalf("core = %q, want %q (normalized %v)", got, tc.wantCore, args)
			}
			if len(ctx.Args) != 1 || ctx.Args[0] != "module.wasm" {
				t.Fatalf("positionals = %v", ctx.Args)
			}
		})
	}

	args, err := cmd.Normalize([]string{"module.wasm", "-p8"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := cmd.Parse("wago run", args)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Str("parallel") != "8" || len(ctx.Args) != 1 {
		t.Fatalf("parallel after module was missed: parallel=%q args=%v", ctx.Str("parallel"), ctx.Args)
	}

	args, err = cmd.Normalize([]string{"module.wasm", "--", "-p8"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = cmd.Parse("wago run", args)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Str("parallel") != "" || len(ctx.Args) != 2 || ctx.Args[1] != "-p8" {
		t.Fatalf("guest -p8 after separator was consumed: parallel=%q args=%v", ctx.Str("parallel"), ctx.Args)
	}
}

func TestRunExecValueMode(t *testing.T) {
	t.Setenv("WAGO_BARE", "1") // exercise the CLI execution path without project/global plugin handoff.
	wasm := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0,
		7, 5, 1, 1, 'f', 0, 0,
		10, 6, 1, 4, 0, 0x41, 7, 0x0b}
	path := filepath.Join(t.TempDir(), "f.wasm")
	if err := os.WriteFile(path, wasm, 0o600); err != nil {
		t.Fatal(err)
	}
	implementation{environment: testEnvironment{}}.Run(command.NewContext([]string{path}, nil, nil))
	implementation{environment: testEnvironment{}}.Run(command.NewContext([]string{path}, nil, map[string]bool{"no-deferred-bounds-checking": true}))
}

func TestRunSuccessWaitsForTeardown(t *testing.T) {
	t.Setenv("WAGO_BARE", "1")
	moduleClosed := make(chan struct{})
	stopStarted := make(chan struct{})
	stopRelease := make(chan struct{})
	stopped := make(chan struct{})
	plugin := &runTeardownPlugin{
		moduleClosed: func() { close(moduleClosed) },
		stop: func(ctx context.Context) error {
			close(stopStarted)
			select {
			case <-stopRelease:
				close(stopped)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	path := filepath.Join(t.TempDir(), "start.wasm")
	if err := os.WriteFile(path, startModule(false), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		implementation{environment: runTeardownEnvironment{plugin: plugin}}.Run(command.NewContext([]string{path}, nil, nil))
		close(done)
	}()
	select {
	case <-stopStarted:
	case <-time.After(5 * time.Second):
		close(stopRelease)
		t.Fatal("run did not start runtime teardown")
	}
	select {
	case <-moduleClosed:
	default:
		close(stopRelease)
		t.Fatal("run started runtime teardown before closing its module")
	}
	select {
	case <-done:
		close(stopRelease)
		t.Fatal("run returned before plugin teardown completed")
	case <-time.After(200 * time.Millisecond):
	}
	close(stopRelease)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after teardown was released")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("run returned without completing plugin teardown")
	}
}

func TestRunTrapWaitsForTeardown(t *testing.T) {
	const childMarker = "WAGO_RUN_TRAP_TEARDOWN_CHILD"
	if os.Getenv(childMarker) == "1" {
		plugin := &runTeardownPlugin{
			moduleClosed: func() {
				_ = os.WriteFile(os.Getenv("WAGO_RUN_TEARDOWN_MODULE_MARKER"), []byte("closed"), 0o600)
			},
			stop: func(context.Context) error {
				return os.WriteFile(os.Getenv("WAGO_RUN_TEARDOWN_STOPPED_MARKER"), []byte("stopped"), 0o600)
			},
		}
		implementation{environment: runTeardownEnvironment{plugin: plugin}}.Run(command.NewContext(
			[]string{os.Getenv("WAGO_RUN_TEARDOWN_MODULE")}, nil, nil,
		))
		return
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "trap.wasm")
	if err := os.WriteFile(path, startModule(true), 0o600); err != nil {
		t.Fatal(err)
	}
	moduleMarker := filepath.Join(dir, "module-closed")
	stoppedMarker := filepath.Join(dir, "stop-finished")
	child := exec.Command(os.Args[0], "-test.run=^TestRunTrapWaitsForTeardown$", "-test.count=1")
	child.Env = append(os.Environ(),
		childMarker+"=1", "WAGO_BARE=1",
		"WAGO_RUN_TEARDOWN_MODULE="+path,
		"WAGO_RUN_TEARDOWN_MODULE_MARKER="+moduleMarker,
		"WAGO_RUN_TEARDOWN_STOPPED_MARKER="+stoppedMarker,
	)
	combined, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("trapping run unexpectedly succeeded\n%s", combined)
	}
	if _, err := os.Stat(moduleMarker); err != nil {
		t.Fatalf("trapping run did not close its module before exit: %v\n%s", err, combined)
	}
	if _, err := os.Stat(stoppedMarker); err != nil {
		t.Fatalf("trapping run exited without completing teardown: %v\n%s", err, combined)
	}
}

func TestRunZeroGuestExitReportsTeardownFailure(t *testing.T) {
	const childMarker = "WAGO_RUN_EXIT_TEARDOWN_CHILD"
	if code := os.Getenv(childMarker); code != "" {
		exitCode := int32(0)
		if code == "7" {
			exitCode = 7
		}
		plugin := &runTeardownPlugin{
			exitCode: &exitCode,
			moduleClosed: func() {
				_ = os.WriteFile(os.Getenv("WAGO_RUN_EXIT_MODULE_MARKER"), []byte("closed"), 0o600)
			},
			stop: func(context.Context) error {
				return errors.New("forced teardown failure")
			},
		}
		implementation{environment: runTeardownEnvironment{plugin: plugin}}.Run(command.NewContext(
			[]string{os.Getenv("WAGO_RUN_EXIT_MODULE")}, nil, nil,
		))
		return
	}
	for _, tc := range []struct {
		guestCode string
		wantCode  int
	}{{"0", 1}, {"7", 7}} {
		t.Run(tc.guestCode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "exit.wasm")
			if err := os.WriteFile(path, exitStartModule(), 0o600); err != nil {
				t.Fatal(err)
			}
			moduleMarker := filepath.Join(dir, "module-closed")
			child := exec.Command(os.Args[0], "-test.run=^TestRunZeroGuestExitReportsTeardownFailure$", "-test.count=1")
			child.Env = append(os.Environ(),
				childMarker+"="+tc.guestCode, "WAGO_BARE=1", "WAGO_RUN_EXIT_MODULE="+path,
				"WAGO_RUN_EXIT_MODULE_MARKER="+moduleMarker,
			)
			combined, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != tc.wantCode {
				t.Fatalf("exit = %v, output = %s; want code %d", err, combined, tc.wantCode)
			}
			if _, err := os.Stat(moduleMarker); err != nil {
				t.Fatalf("guest exit did not close its module: %v\n%s", err, combined)
			}
			if tc.guestCode == "0" && !bytes.Contains(combined, []byte("forced teardown failure")) {
				t.Fatalf("zero guest exit hid teardown failure: %s", combined)
			}
		})
	}
}

func exitStartModule() []byte {
	// (module (import "env" "exit" (func))
	//   (func (export "_start") (call 0)))
	return []byte{0, 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		2, 12, 1, 3, 'e', 'n', 'v', 4, 'e', 'x', 'i', 't', 0, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 1,
		10, 6, 1, 4, 0, 0x10, 0, 0x0b}
}

func startModule(trap bool) []byte {
	body := []byte{0x00, 0x0b}
	if trap {
		body = []byte{0x00, 0x00, 0x0b}
	}
	module := []byte{0x00, 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0,
		10, byte(len(body) + 2), 1, byte(len(body))}
	return append(module, body...)
}

func TestRunExecInvokesReactorInitializerInOrder(t *testing.T) {
	const helper = "WAGO_TEST_RUN_REACTOR_SEQUENCE"
	if os.Getenv(helper) != "" {
		cmd := Command(testEnvironment{})
		args, err := cmd.Normalize([]string{
			os.Getenv(helper), "--invoke", "_initialize", "--invoke", "value",
		})
		if err != nil {
			panic(err)
		}
		ctx, err := cmd.Parse("wago run", args)
		if err != nil {
			panic(err)
		}
		implementation{environment: testEnvironment{}}.Run(ctx)
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "reactor.wasm")
	if err := os.WriteFile(path, reactorModule(), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunExecInvokesReactorInitializerInOrder$", "-test.count=1")
	cmd.Env = append(os.Environ(), helper+"="+path, "WAGO_BARE=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("reactor invocation sequence: %v\n%s", err, output)
	}
	if got := string(output); got != "42\n" {
		t.Fatalf("output = %q, want 42", got)
	}
}

func TestRunExecMultipleInvokesConsumeArgumentsByArity(t *testing.T) {
	const helper = "WAGO_TEST_RUN_INVOKE_SEQUENCE"
	if os.Getenv(helper) != "" {
		cmd := Command(testEnvironment{})
		args, err := cmd.Normalize([]string{
			os.Getenv(helper), "--invoke", "set", "7:i32", "--invoke", "add", "35", "subcommand",
		})
		if err != nil {
			panic(err)
		}
		ctx, err := cmd.Parse("wago run", args)
		if err != nil {
			panic(err)
		}
		implementation{environment: testEnvironment{}}.Run(ctx)
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "sequence.wasm")
	if err := os.WriteFile(path, invokeSequenceModule(), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunExecMultipleInvokesConsumeArgumentsByArity$", "-test.count=1")
	cmd.Env = append(os.Environ(), helper+"="+path, "WAGO_BARE=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ordered invocation: %v\n%s", err, output)
	}
	if got := string(output); got != "42\n" {
		t.Fatalf("output = %q, want 42", got)
	}
}

func TestRunExecGCHeapOverride(t *testing.T) {
	if wago.CoreFeaturesV3&^wago.SupportedFeatures() != 0 {
		t.Skip("complete Core 3 execution is unavailable on this platform")
	}
	t.Setenv("WAGO_BARE", "1")
	// (module (type (array (mut i8))) (func (export "_start")
	//   i32.const 20971520 array.new_default 0 drop))
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x07, 0x02, 0x5e, 0x78, 0x01, 0x60, 0x00,
		0x00, 0x03, 0x02, 0x01, 0x01, 0x07, 0x0a, 0x01,
		0x06, 0x5f, 0x73, 0x74, 0x61, 0x72, 0x74, 0x00,
		0x00, 0x0a, 0x0d, 0x01, 0x0b, 0x00, 0x41, 0x80,
		0x80, 0x80, 0x0a, 0xfb, 0x07, 0x00, 0x1a, 0x0b}
	path := filepath.Join(t.TempDir(), "large-array.wasm")
	if err := os.WriteFile(path, wasm, 0o600); err != nil {
		t.Fatal(err)
	}
	implementation{environment: testEnvironment{}}.Run(command.NewContext(
		[]string{path},
		map[string]string{"core": "3", "gc-heap": "32MiB"},
		nil,
	))
}

func TestRunExecProgramMode(t *testing.T) {
	t.Setenv("WAGO_BARE", "1")
	// (module (func (export "_start")))
	wasm := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0,
		10, 4, 1, 2, 0, 0x0b}
	path := filepath.Join(t.TempDir(), "start.wasm")
	if err := os.WriteFile(path, wasm, 0o600); err != nil {
		t.Fatal(err)
	}
	implementation{environment: testEnvironment{}}.Run(command.NewContext([]string{path, "guest-arg"}, nil, nil))
}

func TestRunValueParsingAndFormatting(t *testing.T) {
	cases := []struct {
		in   string
		typ  wago.ValType
		want string
	}{
		{"-2", wago.ValI32, "-2"},
		{"0xffffffff", wago.ValI32, "-1"},
		{"-3", wago.ValI64, "-3"},
		{"0xffffffffffffffff", wago.ValI64, "-1"},
		{"1.5", wago.ValF32, "1.5"},
		{"2.25", wago.ValF64, "2.25"},
	}
	for _, tc := range cases {
		bits, err := parseVal(tc.in, tc.typ)
		if err != nil {
			t.Errorf("parseVal(%q, %s): %v", tc.in, tc.typ, err)
			continue
		}
		if got := fmtVal(bits, tc.typ); got != tc.want {
			t.Errorf("fmtVal(parseVal(%q, %s)) = %q, want %q", tc.in, tc.typ, got, tc.want)
		}
	}
	for _, tc := range []struct {
		in  string
		typ wago.ValType
	}{{"not-a-number", wago.ValI32}, {"not-a-number", wago.ValI64}, {"nope", wago.ValF32}, {"nope", wago.ValF64}} {
		if _, err := parseVal(tc.in, tc.typ); err == nil {
			t.Errorf("parseVal(%q, %s) accepted invalid value", tc.in, tc.typ)
		}
	}
	_ = mustParseArgs([]string{"7", "1.5:f32"}, []wago.ValType{wago.ValI32, wago.ValI64})
	if got := format([]uint64{wago.I64(9)}, []wago.ValType{wago.ValI64}); got != "9" {
		t.Fatalf("format result = %q", got)
	}
	if got := format(nil, nil); got != "" {
		t.Fatalf("format void = %q", got)
	}
	if got := trapReason(&wago.TrapError{Code: wago.TrapDivZero}); got != "integer division by zero" {
		t.Fatalf("typed trap reason = %q", got)
	}
	if got := trapReason(errors.New("plain error")); got != "plain error" {
		t.Fatalf("plain trap reason = %q", got)
	}
}

func reactorModule() []byte {
	// (module
	//   (global $initialized (mut i32) (i32.const 0))
	//   (func (export "_initialize") (global.set $initialized (i32.const 1)))
	//   (func (export "value") (result i32)
	//     (if (result i32) (global.get $initialized)
	//       (then (i32.const 42)) (else unreachable))))
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02, 0x60,
		0x00, 0x00, 0x60, 0x00, 0x01, 0x7f, 0x03, 0x03, 0x02, 0x00, 0x01, 0x06,
		0x06, 0x01, 0x7f, 0x01, 0x41, 0x00, 0x0b, 0x07, 0x17, 0x02, 0x0b, 0x5f,
		0x69, 0x6e, 0x69, 0x74, 0x69, 0x61, 0x6c, 0x69, 0x7a, 0x65, 0x00, 0x00,
		0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x00, 0x01, 0x0a, 0x14, 0x02, 0x06,
		0x00, 0x41, 0x01, 0x24, 0x00, 0x0b, 0x0b, 0x00, 0x23, 0x00, 0x04, 0x7f,
		0x41, 0x2a, 0x05, 0x00, 0x0b, 0x0b,
	}
}

func invokeSequenceModule() []byte {
	// (module
	//   (global $value (mut i32) (i32.const 0))
	//   (func (export "set") (param i32) (global.set $value (local.get 0)))
	//   (func (export "add") (param i32) (result i32)
	//     (i32.add (global.get $value) (local.get 0))))
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x0a, 0x02, 0x60,
		0x01, 0x7f, 0x00, 0x60, 0x01, 0x7f, 0x01, 0x7f, 0x03, 0x03, 0x02, 0x00,
		0x01, 0x06, 0x06, 0x01, 0x7f, 0x01, 0x41, 0x00, 0x0b, 0x07, 0x0d, 0x02,
		0x03, 0x73, 0x65, 0x74, 0x00, 0x00, 0x03, 0x61, 0x64, 0x64, 0x00, 0x01,
		0x0a, 0x10, 0x02, 0x06, 0x00, 0x20, 0x00, 0x24, 0x00, 0x0b, 0x07, 0x00,
		0x23, 0x00, 0x20, 0x00, 0x6a, 0x0b,
	}
}
