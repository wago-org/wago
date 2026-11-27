package standalone

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/settings"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestParseTargetAndDefaultOutput(t *testing.T) {
	target, err := ParseTarget("windows/arm64")
	if err != nil || target != (Target{OS: "windows", Arch: "arm64"}) {
		t.Fatalf("ParseTarget = %#v, %v", target, err)
	}
	if got := DefaultOutput("hello.wasm", target); got != "hello.exe" {
		t.Fatalf("windows output = %q", got)
	}
	if _, err := ParseTarget("plan9/riscv64"); err == nil {
		t.Fatal("unsupported target was accepted")
	}
}

func TestRequireToolchainExplainsPathRequirement(t *testing.T) {
	originalFind := findTool
	t.Cleanup(func() { findTool = originalFind })
	findTool = func(string) (string, error) { return "", exec.ErrNotFound }
	if err := requireToolchain("go"); err == nil || !strings.Contains(err.Error(), `"go" is available on PATH`) || !strings.Contains(err.Error(), "https://go.dev/dl/") {
		t.Fatalf("Go toolchain error = %v", err)
	}
	if err := requireToolchain("tinygo"); err == nil || !strings.Contains(err.Error(), `"tinygo" is available on PATH`) || !strings.Contains(err.Error(), "https://tinygo.org/getting-started/install/") {
		t.Fatalf("TinyGo toolchain error = %v", err)
	}
}

func TestRequireToolchainAcceptsVersionFoundOnPath(t *testing.T) {
	originalFind := findTool
	t.Cleanup(func() { findTool = originalFind })
	findTool = func(name string) (string, error) { return "/tools/" + name, nil }
	for _, name := range []string{"go", "tinygo"} {
		if err := requireToolchain(name); err != nil {
			t.Fatalf("requireToolchain(%q) = %v", name, err)
		}
	}
}

func TestMainSourceBakesInvokeExport(t *testing.T) {
	request := Request{
		Invoke: "fib", Core: 3, FunctionWorkers: 4, Optimizations: map[string]bool{"inline": false},
	}
	source := string(mainSource(nil, nil, request, true))
	if !strings.Contains(source, `Invoke: "fib", Core: 3, DeferBoundsChecks: false, FunctionWorkers: 4`) ||
		!strings.Contains(source, `"inline": false`) ||
		!strings.Contains(source, `standalone.GenerateModule(module, wago.PluginSet{}, options)`) {
		t.Fatalf("generated main does not invoke fib:\n%s", source)
	}
	bare := string(mainSource(nil, nil, request, false))
	if strings.Contains(bare, "OptimizationKnobs") || !strings.Contains(bare, "standalone.RunCompiledWithoutPlugins") {
		t.Fatalf("bare launcher contains codegen settings or uses the wrong entry point:\n%s", bare)
	}
}

func TestMainSourceUsesRuntimeGeneratedLauncher(t *testing.T) {
	request := Request{Invoke: "run"}
	fallback := string(mainSource(nil, nil, request, false))
	for _, tc := range []struct {
		name        string
		generated   string
		marker      bool
		wantOneShot bool
		want        string
	}{
		{name: "legacy runtime", want: fallback},
		{name: "orphaned marker", marker: true, want: fallback},
		{name: "current runtime", generated: "package main\n", want: "package main\n"},
		{name: "one-shot runtime", generated: "package main\n", marker: true, wantOneShot: true, want: "package main\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			runtime := filepath.Join(dir, "native-runtime")
			for _, subdir := range []string{"src/wago", "internal/buildfeatures"} {
				if err := os.MkdirAll(filepath.Join(runtime, subdir), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, file := range []string{"standalone_module.go.txt", "standalone_profile.go.txt"} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte("generated"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.generated != "" {
				if err := os.WriteFile(filepath.Join(dir, "standalone_launcher.go.txt"), []byte(tc.generated), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.marker {
				if err := os.WriteFile(filepath.Join(dir, "standalone_one_shot.txt"), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			main := filepath.Join(dir, "main.go")
			tags, err := linkStandaloneLauncher(dir, runtime, main, nil, nil, request)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(tags, "wago_standalone_generated") || slices.Contains(tags, "wago_standalone_one_shot") != tc.wantOneShot {
				t.Fatalf("unexpected launcher tags %v", tags)
			}
			source, err := os.ReadFile(main)
			if err != nil || string(source) != tc.want {
				t.Fatalf("launcher = %q, %v, want %q", source, err, tc.want)
			}
		})
	}
}

func TestBuildLinksMachineCodeWithoutCompiler(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	input := filepath.Join(root, "tests", "fixtures", "wasm", "fib.wasm")
	output := filepath.Join(project, DefaultOutput("hello.wasm", host))
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	t.Setenv("GOOS", "windows")
	if host.Arch == "amd64" {
		t.Setenv("GOARCH", "arm64")
	} else {
		t.Setenv("GOARCH", "amd64")
	}
	request := Request{Input: input, Output: output, Target: host, Invoke: "fib", KeepSymbols: true}
	if host.Arch == "amd64" || host.Arch == "arm64" {
		request.Optimizations = map[string]bool{"inline": false}
	}
	result, err := Build(request)
	if err != nil {
		t.Fatal(err)
	}
	names := assertNoCompilerSymbols(t, result.Output)
	assertNoPluginRuntime(t, names)
	if strings.Contains(names, ".rv64ValidateDeclarativeControlTypes") {
		t.Fatal("numeric standalone retained state-only declaration admission")
	}
	if strings.Contains(names, "encoding/json") || strings.Contains(names, "internal/jsonstrict") {
		t.Fatal("plugin-free standalone retained plugin JSON packages")
	}
	if strings.Contains(names, "crypto/rand.") || strings.Contains(names, ".randomNonzeroUint64") {
		t.Fatal("reference-free standalone retained reference token entropy")
	}
	if !strings.Contains(names, ".standaloneNarrowSlotWidths") || strings.Contains(names, ".appendValTypeSlotWidths") {
		t.Fatal("i32 standalone did not specialize its invocation slot widths")
	}
	for _, symbol := range []string{".invokeDirectIntPairEntry", ".invokeDirectIntWideEntry", ".enterNativeIntOctRaw", ".enterNativeFloatRaw", ".enterNativeFloatOctRaw", ".enterNativeMixedRaw"} {
		if strings.Contains(names, symbol) {
			t.Errorf("single-result standalone retained %s", symbol)
		}
	}
	assertNoFunctionImportRuntime(t, names)
	assertNoModuleImportRuntime(t, names)
	assertNoAtomicWaitRuntime(t, names)
	assertNoNativeCollectorRuntime(t, names)
	assertNoSegmentInitialization(t, names)
	assertNoTableOrGlobalRuntime(t, names)
	assertNoReferenceStoreRuntime(t, names)
	assertNoFunctionReexportRuntime(t, names)
	assertNoStartRuntime(t, names)
	assertNoGuardedGrowthRuntime(t, names)
	assertNoSharedMemoryRuntime(t, names)
	assertNoElementValidation(t, names)
	assertNoDynamicSegmentOffsets(t, names)
	assertConstExprFeatureScanner(t, names, false)
	for _, symbol := range []string{".requiredFeaturesForStoredType", ".compiledElementRequiredFeatures", ".aggregateStorageRequiredFeatures", ".validateAggregateTypeFields", "compiledWriter).aggregateTypeFields"} {
		if strings.Contains(names, symbol) {
			t.Errorf("scalar standalone retained %s", symbol)
		}
	}
	assertNoSyncHostControl(t, names)
	assertNoNameSectionClone(t, names)
	binary, err := os.ReadFile(result.Output)
	if err != nil {
		t.Fatal(err)
	}
	wasm, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(binary, wasm) {
		t.Fatal("standalone embeds the original Wasm instead of only its native image")
	}

	command := exec.Command(result.Output, "20")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run standalone: %v\n%s", err, output)
	} else if got := strings.TrimSpace(string(output)); got != "6765" {
		t.Fatalf("standalone output = %q, want 6765", got)
	}
}

func TestBuildTinyGoLinksMachineCodeWithoutCompiler(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !host.supportsTinyGo() {
		t.Skipf("TinyGo standalone is unsupported on %s", host)
	}
	if _, err := exec.LookPath("tinygo"); err != nil {
		t.Skip("tinygo is not installed")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	input := filepath.Join(project, "hello.wasm")
	output := filepath.Join(project, "hello")
	if err := os.WriteFile(input, emptyStartModule(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	t.Setenv("GOOS", "windows")
	if host.Arch == "amd64" {
		t.Setenv("GOARCH", "arm64")
	} else {
		t.Setenv("GOARCH", "amd64")
	}
	result, err := Build(Request{Input: input, Output: output, Target: host, TinyGo: true, KeepSymbols: true})
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(result.Output, "guest-argument").CombinedOutput(); err != nil || len(output) != 0 {
		t.Fatalf("run TinyGo standalone: %v\n%s", err, output)
	}
	names := assertNoCompilerSymbols(t, result.Output)
	for _, symbol := range []string{
		"compiler/frontend.UnsupportedError", "compiler/wasm.ValidationError", "compiler/wasm.DecodeError",
		"compiler/wasm.InstrKind", "compiler/wasm.instrKindNameOffsets",
		"compiler/wasm.ValType",
		"compiler/wasm.NameSec",
		"wago.atomicWaitCloseError",
		"wago.PluginError",
		"wago.CallerHostCallFunc",
		"cli/internal/wasmcall",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("TinyGo scalar standalone retained %s", symbol)
		}
	}
	assertNoPluginRuntime(t, names)
	assertNoFunctionImportRuntime(t, names)
	assertNoModuleImportRuntime(t, names)
	assertNoAtomicWaitRuntime(t, names)
	assertNoNativeCollectorRuntime(t, names)
	assertNoSegmentInitialization(t, names)
	assertNoTableOrGlobalRuntime(t, names)
	assertNoReferenceStoreRuntime(t, names)
	assertNoFunctionReexportRuntime(t, names)
	assertNoStartRuntime(t, names)
	assertNoGuardedGrowthRuntime(t, names)
	assertNoGuardedMemoryRuntime(t, names)
	assertNoSharedMemoryRuntime(t, names)
	assertNoElementValidation(t, names)
	assertNoDynamicSegmentOffsets(t, names)
	assertConstExprFeatureScanner(t, names, false)
	assertNoSyncHostControl(t, names)
	assertNoNameSectionClone(t, names)
}

func TestBuildTinyGoIntegerEntryProfile(t *testing.T) {
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !target.supportsTinyGo() {
		t.Skipf("TinyGo standalone is unsupported on %s", target)
	}
	if _, err := exec.LookPath("tinygo"); err != nil {
		t.Skip("tinygo is not installed")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	for _, tc := range []struct {
		name      string
		direct    bool
		arguments []string
		body      []byte
	}{
		{name: "wrapper", arguments: []string{"42"}, body: []byte{0x20, 0}},
		{name: "zero", direct: true, body: []byte{0x41, 42}},
		{name: "one", direct: true, arguments: []string{"42"}, body: []byte{0x20, 0}},
		{name: "four", direct: true, arguments: []string{"3", "4", "42", "5"}, body: []byte{0x20, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "identity.wasm")
			signature := append([]byte{0x60, byte(len(tc.arguments))}, bytes.Repeat([]byte{0x7f}, len(tc.arguments))...)
			signature = append(signature, 1, 0x7f)
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(signature)),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(append(tc.body, 0x0b)))),
			)
			if err := os.WriteFile(input, source, 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := Build(Request{
				Input: input, Output: filepath.Join(dir, tc.name), Invoke: "run", Target: target,
				TinyGo: true, KeepSymbols: true,
				Optimizations: map[string]bool{"prepared-direct-entry": tc.direct},
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, callBlock := range []string{"0", "1"} {
				command := exec.Command(result.Output, tc.arguments...)
				command.Env = append(os.Environ(), "WAGO_PREPARED_INT_CALL_BLOCK="+callBlock)
				output, err := command.CombinedOutput()
				if err != nil || strings.TrimSpace(string(output)) != "42" {
					t.Fatalf("run with call block %s: %q, %v, want 42", callBlock, output, err)
				}
			}
			names := assertNoCompilerSymbols(t, result.Output)
			hasEntry := strings.Contains(names, ".tinygoARM64IntEntryCode")
			if target.Arch == "amd64" {
				image, err := os.ReadFile(result.Output)
				if err != nil {
					t.Fatal(err)
				}
				// The direct prologue saves linear memory in RBX and
				// writes the foreign-stack reentry pointer through it.
				prologue := []byte{0x48, 0x89, 0xfb, 0x49, 0x8d, 0x7a, 0xf8, 0x48, 0x89, 0x7b, 0xe8}
				hasEntry = bytes.Contains(image, prologue)
			}
			if hasEntry != tc.direct {
				t.Errorf("linked TinyGo integer entry = %v, want %v", hasEntry, tc.direct)
			}
		})
	}
}

func TestBuildCore1StandaloneOmitsProposalRuntime(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	profile := settings.BuildProfile{Core: "1.0"}
	tags, err := profile.Tags()
	if err != nil {
		t.Fatal(err)
	}
	features, err := profile.FeaturesMask()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	input := filepath.Join(root, "tests", "fixtures", "wasm", "fib.wasm")

	for _, toolchain := range []string{"Go", "TinyGo"} {
		t.Run(toolchain, func(t *testing.T) {
			tinyGo := toolchain == "TinyGo"
			if tinyGo {
				if !host.supportsTinyGo() {
					t.Skipf("TinyGo standalone is unsupported on %s", host)
				}
				if _, err := exec.LookPath("tinygo"); err != nil {
					t.Skip("tinygo is not installed")
				}
			}
			output := filepath.Join(t.TempDir(), DefaultOutput("fib.wasm", host))
			result, err := Build(Request{
				Input: input, Output: output, Target: host, Invoke: "fib",
				TinyGo: tinyGo, KeepSymbols: true, BuildTags: tags,
				Features: features, FeaturesSet: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			data, err := exec.Command(result.Output, "20").CombinedOutput()
			if err != nil || strings.TrimSpace(string(data)) != "6765" {
				t.Fatalf("standalone invocation = %q, %v", data, err)
			}
			names := assertNoCompilerSymbols(t, result.Output)
			if tinyGo && strings.Contains(names, " T strconv.FormatFloat") {
				t.Error("integer-result standalone linked float result formatting")
			}
			assertNoPluginRuntime(t, names)
			assertNoInstantiationOptions(t, names)
			for _, symbol := range []string{
				".finishManagedTerminalClose", "ManagedInstance).detachTerminalClose", "ManagedInstance).completeTerminalClose",
				".reserveRuntimeInstance", ".runtimeOwnedNativeMemoryMappings", "runtimeInstanceReservation).release",
				"Instance).startCancellationWatch",
			} {
				if strings.Contains(names, symbol) {
					t.Errorf("bare standalone retained %s", symbol)
				}
			}
			assertNoSnapshotConstruction(t, names)
			if strings.Contains(names, "Compiled).validateStructuralMetadata") {
				t.Error("bare standalone retained structural metadata validation")
			}
			if strings.Contains(names, "Compiled).validateCompactData") {
				t.Error("bare standalone without data retained compact data validation")
			}
			assertNoFunctionImportRuntime(t, names)
			assertNoModuleImportRuntime(t, names)
			assertNoMemoryImportRuntime(t, names)
			for _, symbol := range []string{
				"Compiled).Signature", "/wasmcall.ResolveExport",
				"/wasmcall.ValidateSignature", "/wasmcall.validateSignatureValues",
			} {
				if strings.Contains(names, symbol) {
					t.Errorf("standalone with a baked signature retained %s", symbol)
				}
			}
			for _, symbol := range []string{
				"/gc/native.(*Collector).", "/gc/native.Collector).",
				".registerGCFrameCodeRangeLocked", ".rv64NewDefinedTailGenerationWithBudgets",
				".railshotPrepareNativeTail", ".railshotRetryNativeTail",
				".publishAtomicWaitContext", ".compileGCInitializers",
			} {
				if strings.Contains(names, symbol) {
					t.Errorf("Core 1.0 standalone retained proposal runtime symbol %s", symbol)
				}
			}
		})
	}
}

func TestBuildTinyGoStripsByDefault(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !host.supportsTinyGo() {
		t.Skipf("TinyGo standalone is unsupported on %s", host)
	}
	if _, err := exec.LookPath("tinygo"); err != nil {
		t.Skip("tinygo is not installed")
	}
	if _, err := exec.LookPath("strip"); err != nil {
		t.Skip("strip is not installed")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	input := filepath.Join(project, "hello.wasm")
	output := filepath.Join(project, "hello")
	if err := os.WriteFile(input, emptyStartModule(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	result, err := Build(Request{Input: input, Output: output, Target: host, TinyGo: true})
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(result.Output).CombinedOutput(); err != nil {
		t.Fatalf("run stripped TinyGo standalone: %v\n%s", err, output)
	}
}

func TestBuildRejectsCore3OnUnsupportedTarget(t *testing.T) {
	project := t.TempDir()
	input := filepath.Join(project, "hello.wasm")
	if err := os.WriteFile(input, tailCallStartModule(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(Request{Input: input, Target: Target{OS: "windows", Arch: "amd64"}, Core: 3})
	if err == nil || err.Error() != "WebAssembly Core 3 is not supported for target windows/amd64" {
		t.Fatalf("build error = %v", err)
	}
}

func tailCallStartModule() []byte {
	return []byte{
		'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		3, 3, 2, 0, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 1,
		10, 9, 2, 2, 0, 0x0b, 4, 0, 0x12, 0, 0x0b,
	}
}

func emptyStartModule() []byte {
	return []byte{
		'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0,
		10, 4, 1, 2, 0, 0x0b,
	}
}

func TestBuildStandaloneBakedExport(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), []byte{0x60, 0, 1, 0x7f}, []byte{0x60, 1, 0x7b, 1, 0x7b})),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("_start", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))),
	)
	dir := t.TempDir()
	input := filepath.Join(dir, "exports.wasm")
	if err := os.WriteFile(input, module, 0o644); err != nil {
		t.Fatal(err)
	}
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	selected, err := Build(Request{
		Input: input, Output: filepath.Join(dir, DefaultOutput("selected.wasm", target)), Target: target, Invoke: "run",
	})
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(selected.Output).CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != "42" {
		t.Fatalf("selected export: %q, %v", output, err)
	}
	if output, err := exec.Command(selected.Output, "extra").CombinedOutput(); err == nil || !strings.Contains(string(output), "expected 0 arg(s), got 1") {
		t.Fatalf("extra argument: %q, %v", output, err)
	}
	missing, err := Build(Request{
		Input: input, Output: filepath.Join(dir, DefaultOutput("missing.wasm", target)), Target: target, Invoke: "missing", KeepSymbols: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(missing.Output).CombinedOutput(); err == nil || !strings.Contains(string(output), `no exported function "missing" (have: _start, run)`) {
		t.Fatalf("missing export: %q, %v", output, err)
	}
	assertDiagnosticLauncher(t, assertNoCompilerSymbols(t, missing.Output))
}

func TestBuildStandaloneRejectedInvocation(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	vectorConstant := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	vectorConstant = append(vectorConstant, 0x0b)
	for _, tc := range []struct {
		name   string
		source []byte
		want   string
	}{
		{"no exports", wasmtest.Module(), "module exports no functions"},
		{"unsupported result", wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7b})),
			wasmtest.Section(3, wasmtest.Vec([]byte{0})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(vectorConstant))),
		), "result 0 is v128; unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "rejected.wasm")
			if err := os.WriteFile(input, tc.source, 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := Build(Request{Input: input, Output: filepath.Join(dir, DefaultOutput("rejected.wasm", target)), Target: target, KeepSymbols: true})
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(result.Output, "extra").CombinedOutput()
			want := filepath.Base(result.Output) + ": " + tc.want
			if err == nil || strings.TrimSpace(string(output)) != want {
				t.Fatalf("rejected invocation: %q, %v, want %q", output, err, want)
			}
			assertDiagnosticLauncher(t, assertNoCompilerSymbols(t, result.Output))
		})
	}
}

func TestBuildStandaloneBakedResultTypes(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	for _, result := range []struct {
		name string
		typ  byte
		code []byte
		want string
	}{
		{"i64", 0x7e, []byte{0x42, 0x80, 0x80, 0x80, 0x80, 0x10, 0x0b}, "4294967296"},
		{"f32", 0x7d, []byte{0x43, 0, 0, 0xc0, 0x3f, 0x0b}, "1.5"},
		{"f64", 0x7c, []byte{0x44, 0, 0, 0, 0, 0, 0, 0x02, 0xc0, 0x0b}, "-2.25"},
	} {
		t.Run(result.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, result.typ})),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(result.code))),
			)
			dir := t.TempDir()
			input := filepath.Join(dir, "result.wasm")
			if err := os.WriteFile(input, module, 0o644); err != nil {
				t.Fatal(err)
			}
			built, err := Build(Request{
				Input: input, Output: filepath.Join(dir, DefaultOutput("result.wasm", target)), Target: target,
			})
			if err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(built.Output).CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != result.want {
				t.Fatalf("run: %q, %v, want %q", output, err, result.want)
			}
		})
	}
}

func TestBuildStandaloneBakedScalarSignature(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 3, 0x7e, 0x7d, 0x7c, 3, 0x7c, 0x7e, 0x7d})),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 2, 0x20, 0, 0x20, 1, 0x0b}))),
	)
	dir := t.TempDir()
	input := filepath.Join(dir, "signature.wasm")
	if err := os.WriteFile(input, module, 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := Build(Request{
		Input: input, Output: filepath.Join(dir, DefaultOutput("signature.wasm", target)), Target: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = "-2.25, -4294967296, 1.5"
	if output, err := exec.Command(built.Output, "-4294967296", "1.5", "-2.25").CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != want {
		t.Fatalf("run: %q, %v, want %q", output, err, want)
	}
}

func TestBuildStandaloneDirectIntegerArities(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	arguments := []string{"1", "2", "3", "4", "5", "6"}
	for _, tc := range []struct {
		name            string
		params, results int
	}{
		{"void", 0, 0},
		{"one", 1, 1},
		{"fixed", 4, 1},
		{"pair", 2, 2},
		{"wide", 5, 5},
		{"six-results", 6, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signature := append([]byte{0x60, byte(tc.params)}, bytes.Repeat([]byte{0x7f}, tc.params)...)
			signature = append(signature, byte(tc.results))
			signature = append(signature, bytes.Repeat([]byte{0x7f}, tc.results)...)
			body := make([]byte, 0, 2*tc.results+1)
			for i := tc.params - tc.results; i < tc.params; i++ {
				body = append(body, 0x20, byte(i))
			}
			body = append(body, 0x0b)
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(signature)),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			dir := t.TempDir()
			input := filepath.Join(dir, "arity.wasm")
			if err := os.WriteFile(input, source, 0o644); err != nil {
				t.Fatal(err)
			}
			built, err := Build(Request{Input: input, Output: filepath.Join(dir, DefaultOutput("arity.wasm", target)), Target: target, Invoke: "run", KeepSymbols: true})
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(built.Output, arguments[:tc.params]...).CombinedOutput()
			want := strings.Join(arguments[tc.params-tc.results:tc.params], ", ")
			if err != nil || strings.TrimSpace(string(output)) != want {
				t.Fatalf("run: %q, %v, want %q", output, err, want)
			}
			if target.Arch == "amd64" || target.Arch == "arm64" {
				names := assertNoCompilerSymbols(t, built.Output)
				if tc.name == "wide" {
					assertRegisterBridgeLoads(t, built.Output, target.Arch, ".enterNativeIntWideRaw", 0x1f)
				}
				if tc.name == "six-results" {
					assertRegisterBridgeLoads(t, built.Output, target.Arch, ".enterNativeIntOctRaw", 0x3f)
				}
				for _, symbol := range []string{".enterNativeInt\n", ".enterNativeIntLight\n"} {
					if strings.Contains(names, symbol) {
						t.Errorf("bounded integer signature retained %s", strings.TrimSpace(symbol))
					}
				}
				for _, adapter := range []struct {
					symbol string
					want   bool
				}{
					{".invokeDirectIntEntry", tc.params <= 4 && tc.results <= 1},
					{".invokeCachedDirectI32ToI32", tc.name == "one"},
					{".invokeCachedDirectInt1", false},
					{".invokeDirectIntPairEntry", tc.results == 2},
					{".invokeDirectIntWideEntry", tc.params > 4 || tc.results > 2},
					{".enterNativeIntWideRaw", (tc.params > 4 || tc.results > 2) && tc.results <= 5},
					{".enterNativeIntOctRaw", tc.results > 5},
					{".enterNativeFloatRaw", false},
					{".enterNativeFloatOctRaw", false},
					{".enterNativeMixedRaw", false},
				} {
					if strings.Contains(names, adapter.symbol) != adapter.want {
						t.Errorf("expected linked %s = %v", adapter.symbol, adapter.want)
					}
				}
			}
		})
	}
}

func TestBuildStandaloneFloatRegisterProfiles(t *testing.T) {
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if target.Arch != "arm64" && target.Arch != "amd64" {
		t.Skip("requires native floating-point register adapters")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	for _, tc := range []struct {
		name, adapter string
		types         []byte
		args          []string
	}{
		{"four", ".enterNativeFloatRaw", []byte{0x7c}, []string{"1.5"}},
		{"oct", ".enterNativeFloatOctRaw", bytes.Repeat([]byte{0x7c}, 6), []string{"1.5", "2.25", "3.5", "4.25", "5.5", "6.25"}},
		{"mixed", ".enterNativeMixedRaw", []byte{0x7c, 0x7f}, []string{"1.5", "2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sig := append([]byte{0x60, byte(len(tc.types))}, tc.types...)
			sig = append(append(sig, byte(len(tc.types))), tc.types...)
			var body []byte
			for index := range tc.types {
				body = append(body, 0x20, byte(index))
			}
			// A local i32 identity can have a direct entry without a host export.
			localSig := []byte{0x60, 0x01, 0x7f, 0x01, 0x7f}
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(sig, localSig)),
				wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(
					wasmtest.Code(append(body, 0x0b)),
					wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
				)),
			)
			dir := t.TempDir()
			input := filepath.Join(dir, "registers.wasm")
			if err := os.WriteFile(input, module, 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := Build(Request{Input: input, Output: filepath.Join(dir, DefaultOutput("registers.wasm", target)), Target: target, Invoke: "run", KeepSymbols: true})
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(result.Output, tc.args...).CombinedOutput()
			if want := strings.Join(tc.args, ", "); err != nil || strings.TrimSpace(string(output)) != want {
				t.Fatalf("run: %q, %v, want %q", output, err, want)
			}
			names := assertNoCompilerSymbols(t, result.Output)
			for _, adapter := range []string{".enterNativeFloatRaw", ".enterNativeFloatOctRaw", ".enterNativeMixedRaw"} {
				if strings.Contains(names, adapter) != (adapter == tc.adapter) {
					t.Errorf("expected linked %s = %v", adapter, adapter == tc.adapter)
				}
			}
			lanes := uint8(1<<len(tc.args) - 1)
			if tc.adapter == ".enterNativeMixedRaw" {
				lanes = 1<<0 | 1<<4
			}
			assertRegisterBridgeLoads(t, result.Output, target.Arch, tc.adapter, lanes)
			for _, adapter := range []string{
				".invokeDirectIntEntry",
				".invokeCachedDirectI32ToI32",
				".invokeCachedDirectInt1",
				".invokeDirectIntPairEntry",
				".invokeDirectIntWideEntry",
				".enterNativeIntWideRaw",
				".enterNativeIntOctRaw",
			} {
				if strings.Contains(names, adapter) {
					t.Errorf("floating signature retained %s", adapter)
				}
			}
		})
	}
}

func assertRegisterBridgeLoads(t *testing.T, executable, arch, symbol string, lanes uint8) {
	t.Helper()
	assembly, err := exec.Command("go", "tool", "objdump", "-s", `\`+symbol+`\.abi0$`, executable).CombinedOutput()
	if err != nil {
		t.Fatalf("disassemble register bridge: %v\n%s", err, assembly)
	}
	for lane := 0; lane < 8; lane++ {
		want := lanes&(1<<lane) != 0
		offset := ""
		if lane != 0 {
			offset = fmt.Sprint(8 * lane)
		}
		load := "MOVD " + offset + "(R11),"
		if arch == "amd64" {
			load = fmt.Sprintf("MOVQ %#x(DI),", 8*lane)
			if lane == 0 {
				load = "MOVQ 0(DI),"
			}
		}
		if bytes.Contains(assembly, []byte(load)) != want {
			t.Errorf("expected register bridge load %q = %v", load, want)
		}
	}
}

func TestGeneratedSourcesBakeFeatures(t *testing.T) {
	request := Request{DeferredBoundsChecking: true, FeaturesSet: true}
	for _, generate := range []bool{true, false} {
		source := mainSource(nil, nil, request, generate)
		if !strings.Contains(string(source), "Features: 0, FeaturesSet: true") {
			t.Fatalf("generated source lost explicit empty feature mask:\n%s", source)
		}
	}
}

func TestNativeLauncherSelectsPluginEntryPoint(t *testing.T) {
	bare := string(mainSource(nil, nil, Request{}, false))
	if !strings.Contains(bare, "standalone.RunCompiledWithoutPlugins(") || strings.Contains(bare, "encoding/json") {
		t.Fatalf("bare launcher has unexpected dependencies:\n%s", bare)
	}
	withImports := string(mainSource([]string{"example.com/plugin/register"}, []byte("null"), Request{}, false))
	withSelections := string(mainSource(nil, []byte(`[{"id":"example.com/plugin"}]`), Request{}, false))
	for _, source := range []string{withImports, withSelections} {
		if !strings.Contains(source, "standalone.RunCompiled(wago.StandaloneCompiled(), pluginSet()") ||
			!strings.Contains(source, "json.Unmarshal") {
			t.Fatalf("plugin launcher did not retain plugin loading:\n%s", source)
		}
	}
}

func TestLinuxRISCV64StandaloneTarget(t *testing.T) {
	target, err := ParseTarget("linux/riscv64")
	if err != nil {
		t.Fatal(err)
	}
	if !target.supportsCore3() {
		t.Fatal("riscv64 Core 3 standalone is disabled")
	}
	if target.supportsTinyGo() {
		t.Fatal("riscv64 TinyGo standalone is enabled")
	}
	for _, goos := range []string{"darwin", "windows"} {
		if _, err := ParseTarget(goos + "/riscv64"); err == nil {
			t.Fatalf("accepted %s/riscv64", goos)
		}
	}
}

func TestBuildRejectsNonNativeTarget(t *testing.T) {
	project := t.TempDir()
	input := filepath.Join(project, "hello.wasm")
	if err := os.WriteFile(input, emptyStartModule(), 0o644); err != nil {
		t.Fatal(err)
	}
	target := Target{OS: "linux", Arch: "amd64"}
	if target == (Target{OS: runtime.GOOS, Arch: runtime.GOARCH}) {
		target = Target{OS: "linux", Arch: "arm64"}
	}
	_, err := Build(Request{Input: input, Target: target})
	if err == nil || !strings.Contains(err.Error(), "standalone builds require the native target") {
		t.Fatalf("build error = %v", err)
	}
}

func assertNoCompilerSymbols(t *testing.T, executable string) string {
	t.Helper()
	names, err := exec.Command("go", "tool", "nm", executable).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect standalone symbols: %v\n%s", err, names)
	}
	for _, forbidden := range []string{
		"compiler/backend/railshot/amd64.", "compiler/backend/railshot/arm64.", "compiler/backend/railshot/riscv64.Compile",
		"compiler/wasm.DecodeModule", "compiler/wasm.decodeModule", "compiler/frontend.Lower", "standalonegen.",
		"compiler/wasm.(*funcValidator).", "compiler/wasm.funcValidator).",
		"compiler/wasm.ClassifyInstructionImmediate", "compiler/wasm.classifyExprOpAfterOpcode",
		"compiler/wasm.classifyFCBytes", "compiler/wasm.classifyFEBytes",
		"compiler/wasm.fillEncoderOpcodes", "compiler/wasm.fillScalarEffects", "compiler/wasm.fillSIMDEffects",
		"compiler/wasm.simpleKindOpcode", "compiler/wasm.memKindOpcode", "compiler/wasm.opEffects", "compiler/wasm.simdEffects",
		"compiler/wasm.effectValTypes",
		"/runtime.NewCodeBuffer", "/runtime.(*CodeBuffer).", "/runtime.CodeBuffer).",
	} {
		if strings.Contains(string(names), forbidden) {
			for _, line := range strings.Split(string(names), "\n") {
				if strings.Contains(line, forbidden) {
					t.Errorf("standalone retains compiler: %s", line)
					break
				}
			}
		}
	}
	return string(names)
}

func assertDiagnosticLauncher(t *testing.T, names string) {
	t.Helper()
	for _, line := range strings.Split(names, "\n") {
		if strings.Contains(line, "github.com/wago-org/wago/src/") ||
			strings.Contains(line, ".StandaloneCompiled") ||
			(strings.Contains(line, "github.com/wago-org/wago/cli/") &&
				!strings.Contains(line, "github.com/wago-org/wago/cli/standalone/arguments.")) {
			t.Errorf("diagnostic launcher linked %s", line)
			return
		}
	}
}

func assertNoPluginRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".(*Runtime).LoadPlugins", ".finishCloseAfterLoading", ".stopPluginRun", ".drainPluginRun", ".finishPluginRun",
		".instantiateWithLifecycleHooks", ".emitInstantiationError", ".beginModuleCloseCallbacks",
		".initializePluginRuntime", ".validatePluginImportOverrides",
		".standaloneManagedDispatcher", ".standaloneDispatcherFactory",
		"/wago.pluginCallGate", "(*pluginCallGate).",
		"pluginOperationReservation).release", ".reservePluginOperation",
		".registeredPluginRun", "/wago.pluginRunList", "/wago.pluginOperationReservation",
		"/wago.invocationReservations", "/wago.pluginCallbackGate", "/wago.pluginOperationGates",
		"/wago.PluginDefinition", "/wago.PluginProvenance", "/wago.PluginRequirement",
		".WithGuestArguments", "guestArguments:slice:basic:string",
		"/wago.registeredImport", "/wago.hookRegistry",
		"/wago.registeredInstruction", "/wago.pluginGCImportState",
		".gatedHostCallFunc", ".gatedOrdinaryHostFunc", ".gatedI32HostEvent",
		".gatedNoArgsHostFunc", ".gatedI32HostFunc", ".gatedI32ToI32HostFunc",
		".gatedI32I32HostFunc", ".gatedI32I32ToI32HostFunc",
		".gatedI32ToI32I32HostFunc", ".gatedI32I32ToI32I32HostFunc",
		"compiler/optimization.",
	} {
		for _, line := range strings.Split(names, "\n") {
			if strings.Contains(line, symbol) {
				t.Errorf("bare standalone retained %s", line)
				break
			}
		}
	}
}

func assertNoFunctionImportRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".buildHostFuncThunks", ".sharedHostFuncThunks", ".buildSyncHosts", ".buildHostEvents",
		".releaseHostThunksLocked", ".releaseFailedMappingsLocked", ".releaseOwnedCodeMapping",
		".prepareNativeImportReentries",
		".tryInvokeCachedHostScalar1", ".tryInvokeCachedBoundedNumeric", ".callCachedBoundedTypedHost",
		".callPreparedHostSync", ".replayHostLog",
		"/runtime/hostthunk.", "/runtime.RegisterNativeScalarLeaf",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone retained function import runtime %s", symbol)
		}
	}
}

func assertNoModuleImportRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".resolveInstantiationImports", ".resolveInstanceImports", ".indexDeclaredImportIdentities",
		".preflightImportBindings", ".attachImports", ".importedGlobals",
		".detachImportedGlobals", ".detachImportedFunctions", ".detachImportedTables", ".detachImportedTags",
		".validateImportBindingsWithPluginGC",
		".(*Memory).validateLimits",
		".dispatchSyncHostScalar", ".dispatchSyncHostReference",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without module imports retained %s", symbol)
		}
	}
	if runtime.GOARCH != "riscv64" {
		for _, symbol := range []string{".functionImportBindingKey", ".importModuleEndSections", ".splitImportKeyAt", ".validateImportModuleEnd"} {
			if strings.Contains(names, symbol) {
				t.Errorf("standalone without import names retained %s", symbol)
			}
		}
	}
	assertNoFuncrefStorageImportRuntime(t, names)
}

func assertNoFuncrefStorageImportRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".retainProducerRootsInImported", ".importedFuncrefProducerRoots", ".transferImportedAttachmentsFromOwner",
		".funcrefImportContainers.func", ".(*Instance).funcrefImportContainers",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without imported funcref storage retained %s", symbol)
		}
	}
}

func assertNoMemoryImportRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".memoryImportAt", ".memoryImportBindingKey", ".memoryImportCount",
		"memoryImportAttachments).", "Memory).detachImporter", ".transferImportedMemoryAttachment", "memoryImporterTable).",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without memory imports retained %s", symbol)
		}
	}
}

func assertNoTagImportRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{".attachTagImports", ".detachImportedTags", ".tagImportBindingKey", "tagImportAttachments)."} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without tag imports retained %s", symbol)
		}
	}
}

func assertNoAtomicWaitRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".dispatchAtomicWaitHelper", ".dispatchSharedAtomicWait", ".publishAtomicWaitContext",
		".cancelAtomicWaitContext", ".setAtomicWaitHelperPanicError",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without atomic helpers retained %s", symbol)
		}
	}
}

func assertNoNativeCollectorRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".dispatchGCHelperParked", ".prepareCollector", ".acquireGCCollector", "/gc/native.NewCollector",
		"gcInvocationDomainView.", "gcInvocationDomainView).",
		".registerGCFrameCodeRangeLocked", ".unregisterGCFrameCodeRangeLocked",
		".releaseGCRef",
		"gcExternConversionState).", ".newGCExternConversionState",
		"stagedGCArrayProduct).String", "stagedGCStructProduct).String", "stagedGCI31Product).String",
		".stagedGCArrayProductName",
		".managedFunctionAfterInvocationUnlock", ".managedFunctionDeferFinalization",
		"/gc/native.(*Collector).", "/gc/native.Collector).",
		"/gc/native.validateStructDesc",
		"/gc/native.(*TypeDesc).PrepareScanPlan", "/gc/native.TypeDesc).PrepareScanPlan", "/gc/native.init",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without a collector retained %s", symbol)
		}
	}
}

func assertNoSyncHostControl(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".callNativeSync", ".prepareHostReentryState", "hostLoopActivation).dispatch",
		".registerHostControl", ".unregisterHostControl", ".recoverNativeSyncPanicCompletion",
		".HostCtrlFrameBytesForSlots", ".InitHostCtrlFrame", ".RegisterHostCtrlFrame", ".UnregisterHostCtrlFrame",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without synchronous host control retained %s", symbol)
		}
	}
}

func assertNoSegmentInitialization(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".instantiateSecondaryMemories", ".initializeOwnedActiveData", ".applyActiveData",
		".preflightCore1DataSegments", ".preflightCore1ElementSegments",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without data or element segments retained %s", symbol)
		}
	}
}

func assertNoTableOrGlobalRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".(*Table).releaseRetainedInstances", ".(*Global).instanceOwnerClosed", ".validateRuntimeReferenceGlobalMetadata",
		".Table).releaseRetainedInstances", ".Global).instanceOwnerClosed",
		".evalCompiledScalarConstExpr", ".validateCompiledScalarConstExpr", ".validateGlobalInitExpr",
		".validateTableDeclarations", ".validateTableBindings", ".stagedInertOversizedTable64",
		".tableRuntimeCapacity", ".hasFuncrefTable", ".tableElementType", ".tableImportCount",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("scalar standalone retained %s", symbol)
		}
	}
}

func assertNoReferenceStoreRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		"referenceStore).",
		".marshalPublicReferenceArgs", ".translatePublicReferenceResults",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without references retained %s", symbol)
		}
	}
}

func assertNoFunctionReexportRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{".invokeReexportedHost", ".invokeAttachedLocalContext", ".railshotInvokeNativeReexport"} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without function re-exports retained %s", symbol)
		}
	}
}

func assertNoStartRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{".invokeVoidEntry", ".callVoidNative", ".callImportedStart", ".railshotInvokeNativeImportedStart"} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without a start section retained %s", symbol)
		}
	}
}

func assertNoGuardedGrowthRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{".commitGuardedGrowth", ".invokeGuardedMemoryGrow"} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without guarded memory growth retained %s", symbol)
		}
	}
}

func assertNoGuardedMemoryRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{".newGuardedJobMemory", ".CallGuarded", ".InstallGuardTrapHandler"} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without guarded memory retained %s", symbol)
		}
	}
}

func assertNoGuestMemoryAllocator(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{".AcquireJobMemoryGrowable", ".NewJobMemoryGrowable", ".ReleaseJobMemoryWithInitializedRange"} {
		if strings.Contains(names, symbol+"\n") {
			t.Errorf("standalone without guest memory retained %s", symbol)
		}
	}
}

func assertNoSharedMemoryRuntime(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{"Memory).share", ".prepareThreadedExecution", ".lockThreadedInstanceState"} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without shared memory retained %s", symbol)
		}
	}
}

func assertNoElementValidation(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{".validateElementValues", ".validateGCConstExpr", ".evalCompiledGCConstExpr"} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without element segments or GC expressions retained %s", symbol)
		}
	}
}

func assertNoDynamicSegmentOffsets(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{".activeSegmentOffset", ".validateDeferredOffsetGlobal"} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without dynamic segment offsets retained %s", symbol)
		}
	}
}

func assertConstExprFeatureScanner(t *testing.T, names string, want bool) {
	t.Helper()
	if linked := strings.Contains(names, ".requiredFeaturesForConstExprBytes"); linked != want {
		t.Errorf("constant-expression feature scanner linked = %t, want %t", linked, want)
	}
}

func assertNoNameSectionClone(t *testing.T, names string) {
	t.Helper()
	if strings.Contains(names, ".cloneCompiledNames") {
		t.Error("bare standalone retained Wasm name section cloning")
	}
}

func TestBuildStandaloneTrapNames(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("requires native Wasm trap frames")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	for _, name := range []string{"", "wasm_debug_name"} {
		want := "run"
		if name != "" {
			want = name
		}
		t.Run(want, func(t *testing.T) {
			sections := [][]byte{
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))),
			}
			if name != "" {
				names := wasmtest.NameMap(wasmtest.NameAssoc{Index: 0, Name: name})
				sections = append(sections, wasmtest.Custom("name", wasmtest.NameSubsection(1, names)))
			}
			dir := t.TempDir()
			input := filepath.Join(dir, "trap.wasm")
			if err := os.WriteFile(input, wasmtest.Module(sections...), 0o644); err != nil {
				t.Fatal(err)
			}
			target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
			result, err := Build(Request{
				Input: input, Output: filepath.Join(dir, DefaultOutput("trap.wasm", target)),
				Target: target, Invoke: "run", KeepSymbols: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(result.Output).CombinedOutput()
			if err == nil || !strings.Contains(string(output), "at "+want+" (func[0]") {
				t.Fatalf("standalone trap = %q, %v, want frame named %s", output, err, want)
			}
			assertNoNameSectionClone(t, assertNoCompilerSymbols(t, result.Output))
		})
	}
}

func TestBuildStandaloneGuardedGrowthProfile(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" || !wago.GuardPageSupported() {
		t.Skip("requires Darwin ARM64 guarded memory growth")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	input := filepath.Join(t.TempDir(), "grow.wasm")
	// (module (memory 1 2) (func (export "run") (result i32)
	//   (drop (memory.grow (i32.const 1))) (memory.size)))
	source := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0, 1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0, 5, 4, 1, 1, 1, 2, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0,
		10, 11, 1, 9, 0, 0x41, 1, 0x40, 0, 0x1a, 0x3f, 0, 0x0b,
	}
	if err := os.WriteFile(input, source, 0o644); err != nil {
		t.Fatal(err)
	}
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	t.Run("no linear memory", func(t *testing.T) {
		t.Setenv("WAGO_BOUNDS", "signals")
		dir := t.TempDir()
		input := filepath.Join(dir, "no-memory.wasm")
		if err := os.WriteFile(input, emptyStartModule(), 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := Build(Request{
			Input: input, Output: filepath.Join(dir, DefaultOutput("no-memory.wasm", target)), Target: target, KeepSymbols: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(result.Output).CombinedOutput(); err != nil {
			t.Fatalf("run standalone without memory: %v\n%s", err, output)
		}
		names := assertNoCompilerSymbols(t, result.Output)
		assertNoGuardedMemoryRuntime(t, names)
		assertNoGuardedGrowthRuntime(t, names)
		assertNoGuestMemoryAllocator(t, names)
	})
	for _, mode := range []string{"explicit", "signals"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("WAGO_BOUNDS", mode)
			result, err := Build(Request{
				Input: input, Output: filepath.Join(t.TempDir(), DefaultOutput("grow.wasm", target)), Target: target,
				Invoke: "run", KeepSymbols: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(result.Output).CombinedOutput()
			if err != nil || strings.TrimSpace(string(output)) != "2" {
				t.Fatalf("standalone memory growth (%s): %q, %v", mode, output, err)
			}
			names := assertNoCompilerSymbols(t, result.Output)
			if mode == "signals" {
				if !strings.Contains(names, ".commitGuardedGrowth") || !strings.Contains(names, ".newGuardedJobMemory") {
					t.Fatal("standalone omitted guarded memory or its growth commit")
				}
			} else {
				assertNoGuardedGrowthRuntime(t, names)
				assertNoGuardedMemoryRuntime(t, names)
			}
		})
	}
}

func TestBuildStandaloneWithLocalStart(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	dir := t.TempDir()
	input := filepath.Join(dir, "start.wasm")
	// (module (global (mut i32) (i32.const 0))
	//   (func $start (global.set 0 (i32.const 42))) (start $start)
	//   (func (export "run") (result i32) (global.get 0)))
	source := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0, 1, 8, 2, 0x60, 0, 0, 0x60, 0, 1, 0x7f,
		3, 3, 2, 0, 1, 6, 6, 1, 0x7f, 1, 0x41, 0, 0x0b,
		7, 7, 1, 3, 'r', 'u', 'n', 0, 1, 8, 1, 0,
		10, 13, 2, 6, 0, 0x41, 42, 0x24, 0, 0x0b, 4, 0, 0x23, 0, 0x0b,
	}
	if err := os.WriteFile(input, source, 0o644); err != nil {
		t.Fatal(err)
	}
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	for _, toolchain := range []string{"Go", "TinyGo"} {
		t.Run(toolchain, func(t *testing.T) {
			tinyGo := toolchain == "TinyGo"
			if tinyGo {
				if !target.supportsTinyGo() {
					t.Skipf("TinyGo standalone is unsupported on %s", target)
				}
				if _, err := exec.LookPath("tinygo"); err != nil {
					t.Skip("tinygo is not installed")
				}
			}
			result, err := Build(Request{
				Input: input, Output: filepath.Join(t.TempDir(), DefaultOutput("start.wasm", target)), Target: target,
				Invoke: "run", KeepSymbols: true, TinyGo: tinyGo,
			})
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(result.Output).CombinedOutput()
			if err != nil || strings.TrimSpace(string(output)) != "42" {
				t.Fatalf("standalone local start: %q, %v", output, err)
			}
			names := assertNoCompilerSymbols(t, result.Output)
			assertNoGuestMemoryAllocator(t, names)
			if !tinyGo && strings.Contains(names, ".(*Instance).nextInvokeCacheSlot\n") {
				t.Error("one-shot launcher retained export-cache rotation")
			}
			if strings.Contains(names, ".selectExecutionFeatures\n") {
				t.Error("standalone retained runtime selection of the baked Core feature mask")
			}
			if strings.Contains(names, ".callImportedStart") || strings.Contains(names, ".railshotInvokeNativeImportedStart") {
				t.Fatal("local start linked imported start dispatch")
			}
			if strings.Contains(names, "Instance).startCancellationWatch") {
				t.Fatal("bare local start linked the context cancellation watcher")
			}
		})
	}
}

func TestBuildStandaloneWithDeferredGlobalInitializer(t *testing.T) {
	features := wago.CoreFeaturesV2 | wago.CoreFeatureExtendedConst | wago.CoreFeatureExtendedConstExpressions
	if err := wago.NewRuntimeConfig().WithCoreFeatures(features).Validate(); err != nil {
		t.Skipf("requires extended constant expressions: %v", err)
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	dir := t.TempDir()
	input := filepath.Join(dir, "globals.wasm")
	// (module (global i32 (i32.const 40))
	//   (global i32 (i32.add (global.get 0) (i32.const 3)))
	//   (func (export "run") (result i32) (global.get 1)))
	source := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0, 1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0, 6, 14, 2, 0x7f, 0, 0x41, 40, 0x0b, 0x7f, 0, 0x23, 0, 0x41, 3, 0x6a, 0x0b,
		7, 7, 1, 3, 'r', 'u', 'n', 0, 0, 10, 6, 1, 4, 0, 0x23, 1, 0x0b,
	}
	if err := os.WriteFile(input, source, 0o644); err != nil {
		t.Fatal(err)
	}
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	result, err := Build(Request{
		Input: input, Output: filepath.Join(dir, DefaultOutput("globals.wasm", target)), Target: target,
		Invoke: "run", Features: features, FeaturesSet: true, KeepSymbols: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(result.Output).CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "43" {
		t.Fatalf("standalone deferred global initializer: %q, %v", output, err)
	}
	names := assertNoCompilerSymbols(t, result.Output)
	if !strings.Contains(names, ".evalCompiledScalarConstExpr") {
		t.Fatal("standalone with a deferred initializer omitted its evaluator")
	}
	if strings.Contains(names, ".rv64StateOnlyExternrefGlobals") {
		t.Fatal("standalone with executable code retained the state-only externref classifier")
	}
	if strings.Contains(names, ".rv64ValidatePlainReferenceTableDirectory") || strings.Contains(names, ".rv64TableWitnessError") {
		t.Fatal("standalone without tables retained table directory admission")
	}
	for _, symbol := range []string{".rv64MemoryDeclarationError", ".rv64DataInitializerError", ".rv64DataBudgetError"} {
		if strings.Contains(names, symbol) {
			t.Errorf("memory-free standalone retained %s", symbol)
		}
	}
	if runtime.GOARCH != "riscv64" {
		assertConstExprFeatureScanner(t, names, false)
	}
	assertNoReferenceStoreRuntime(t, names)
	assertNoDynamicSegmentOffsets(t, names)
}

func TestBuildNativeAtomicWaitProfile(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	features := wago.CoreFeaturesV2 | wago.CoreFeatureThreads
	if err := wago.NewRuntimeConfig().WithCoreFeatures(features).Validate(); err != nil {
		t.Skipf("requires native Wasm threads: %v", err)
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	t.Setenv("WAGO_BOUNDS", "explicit")
	for _, tc := range []struct {
		name            string
		module          []byte
		args            []string
		result          string
		wait32          bool
		wait64          bool
		notify          bool
		grows           bool
		inline          bool
		indexedMemory64 bool
	}{
		// (module (memory 1 1 shared) (func (export "run") (result i32)
		//   (i32.atomic.load (i32.const 0))))
		{name: "direct load", result: "0", module: []byte{
			0, 'a', 's', 'm', 1, 0, 0, 0, 1, 5, 1, 0x60, 0, 1, 0x7f,
			3, 2, 1, 0, 5, 4, 1, 3, 1, 1, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0,
			10, 10, 1, 8, 0, 0x41, 0, 0xfe, 0x10, 2, 0, 0x0b,
		}},
		// (module (memory 1 1 shared) (func (export "run") (param i32 i64) (result i32)
		//   (memory.atomic.wait32 (i32.const 0) (local.get 0) (local.get 1))))
		{name: "wait32", args: []string{"0", "0"}, result: "2", wait32: true, module: []byte{
			0, 'a', 's', 'm', 1, 0, 0, 0, 1, 7, 1, 0x60, 2, 0x7f, 0x7e, 1, 0x7f,
			3, 2, 1, 0, 5, 4, 1, 3, 1, 1, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0,
			10, 14, 1, 12, 0, 0x41, 0, 0x20, 0, 0x20, 1, 0xfe, 1, 2, 0, 0x0b,
		}},
		// (module (memory 1 1 shared) (func (export "run") (param i64 i64) (result i32)
		//   (memory.atomic.wait64 (i32.const 0) (local.get 0) (local.get 1))))
		{name: "wait64", args: []string{"0", "0"}, result: "2", wait64: true, module: []byte{
			0, 'a', 's', 'm', 1, 0, 0, 0, 1, 7, 1, 0x60, 2, 0x7e, 0x7e, 1, 0x7f,
			3, 2, 1, 0, 5, 4, 1, 3, 1, 1, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0,
			10, 14, 1, 12, 0, 0x41, 0, 0x20, 0, 0x20, 1, 0xfe, 2, 3, 0, 0x0b,
		}},
		// (module (memory 1 1 shared) (func (export "run") (param i32) (result i32)
		//   (memory.atomic.notify (i32.const 0) (local.get 0))))
		{name: "notify", args: []string{"1"}, result: "0", notify: true, module: []byte{
			0, 'a', 's', 'm', 1, 0, 0, 0, 1, 6, 1, 0x60, 1, 0x7f, 1, 0x7f,
			3, 2, 1, 0, 5, 4, 1, 3, 1, 1, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0,
			10, 12, 1, 10, 0, 0x41, 0, 0x20, 0, 0xfe, 0, 2, 0, 0x0b,
		}},
		// (module (memory 1 2 shared) (func (export "run") (param i32) (result i32)
		//   (memory.grow (local.get 0))))
		{name: "shared growth", args: []string{"1"}, result: "1", grows: true, module: []byte{
			0, 'a', 's', 'm', 1, 0, 0, 0, 1, 6, 1, 0x60, 1, 0x7f, 1, 0x7f,
			3, 2, 1, 0, 5, 4, 1, 3, 1, 2, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0,
			10, 8, 1, 6, 0, 0x20, 0, 0x40, 0, 0x0b,
		}},
		// The zero-time wait can compare in native code without a parked call.
		{name: "inline wait32", result: "2", inline: true, module: []byte{
			0, 'a', 's', 'm', 1, 0, 0, 0, 1, 5, 1, 0x60, 0, 1, 0x7f,
			3, 2, 1, 0, 5, 4, 1, 3, 1, 1, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0,
			10, 14, 1, 12, 0, 0x41, 0, 0x41, 0, 0x42, 0, 0xfe, 1, 2, 0, 0x0b,
		}},
		// Memory 0 is unshared. The helper must wait on shared Memory64 at index 1.
		{name: "indexed memory64 wait64", args: []string{"0", "0", "0"}, result: "2", wait64: true, indexedMemory64: true,
			module: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 3, 0x7e, 0x7e, 0x7e, 1, 0x7f})),
				wasmtest.Section(3, []byte{1, 0}),
				wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1}, []byte{7, 1, 1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfe, 2, 0x43, 1, 0, 0x0b}))),
			)},
		{name: "indexed memory64 growth", args: []string{"1"}, result: "1", grows: true, indexedMemory64: true,
			module: wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 1, 0x7e, 1, 0x7e})),
				wasmtest.Section(3, []byte{1, 0}),
				wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1}, []byte{7, 1, 2})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x40, 1, 0x0b}))),
			)},
		// Drop the wait status and grow the same shared memory.
		{name: "wait and growth", args: []string{"1"}, result: "1", wait32: true, grows: true, module: []byte{
			0, 'a', 's', 'm', 1, 0, 0, 0, 1, 6, 1, 0x60, 1, 0x7f, 1, 0x7f,
			3, 2, 1, 0, 5, 4, 1, 3, 1, 2, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0,
			10, 19, 1, 17, 0, 0x41, 0, 0x41, 1, 0x42, 1, 0xfe, 1, 2, 0, 0x1a, 0x20, 0, 0x40, 0, 0x0b,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imageFeatures := features
			if tc.indexedMemory64 {
				imageFeatures |= wago.CoreFeatureMultiMemory | wago.CoreFeatureMemory64
				if err := wago.NewRuntimeConfig().WithCoreFeatures(imageFeatures).Validate(); err != nil {
					t.Skipf("requires indexed Memory64: %v", err)
				}
			}
			var optimizations map[string]bool
			if tc.inline {
				if runtime.GOARCH == "riscv64" {
					t.Skip("zero-time wait lowering requires AMD64 or ARM64")
				}
				optimizations = map[string]bool{"atomic-wait-zero-timeout": true}
			}
			dir := t.TempDir()
			input := filepath.Join(dir, "atomic.wasm")
			if err := os.WriteFile(input, tc.module, 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := Build(Request{
				Input: input, Output: filepath.Join(dir, DefaultOutput("atomic.wasm", host)), Target: host,
				Invoke: "run", Features: imageFeatures, FeaturesSet: true, KeepSymbols: true,
				Optimizations: optimizations,
			})
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(result.Output, tc.args...).CombinedOutput()
			if err != nil || strings.TrimSpace(string(output)) != tc.result {
				t.Fatalf("atomic standalone: %q, %v", output, err)
			}
			if tc.indexedMemory64 && tc.wait64 {
				output, err := exec.Command(result.Output, "4294967296", "0", "0").CombinedOutput()
				if err == nil || !bytes.Contains(output, []byte("linear memory access out of bounds")) {
					t.Fatalf("indexed Memory64 address was not preserved: %q, %v", output, err)
				}
			}
			if tc.indexedMemory64 && tc.grows {
				output, err := exec.Command(result.Output, "4294967296").CombinedOutput()
				if err != nil || strings.TrimSpace(string(output)) != "-1" {
					t.Fatalf("indexed Memory64 growth overflow: %q, %v", output, err)
				}
			}
			names := assertNoCompilerSymbols(t, result.Output)
			if !strings.Contains(names, "Memory).share") || !strings.Contains(names, ".prepareThreadedExecution") {
				t.Fatal("standalone with shared memory omitted shared memory setup")
			}
			assertNoMemoryImportRuntime(t, names)
			assertNoNativeCollectorRuntime(t, names)
			assertNoReferenceStoreRuntime(t, names)
			waits := tc.wait32 || tc.wait64
			if waits || tc.notify || tc.grows {
				if !strings.Contains(names, ".dispatchAtomicWaitHelperContext") || !strings.Contains(names, ".InitHostCtrlFrame") {
					t.Fatal("standalone atomic wait omitted its runtime helper")
				}
				if strings.Contains(names, ".registerHostControl") || strings.Contains(names, ".RegisterHostCtrlFrame") {
					t.Fatal("standalone atomic helper retained foreign host frame registration")
				}
			} else {
				assertNoAtomicWaitRuntime(t, names)
				assertNoSyncHostControl(t, names)
			}
			if strings.Contains(names, ".growShared") != tc.grows {
				t.Fatal("unexpected shared growth linkage")
			}
			for _, symbol := range []string{".closeWaitersLocked", ".waiterStateLocked", ".publishAtomicWaitContext"} {
				if strings.Contains(names, symbol) != waits {
					t.Errorf("unexpected atomic waiter linkage for %s", symbol)
				}
			}
			binary, err := os.ReadFile(result.Output)
			if err != nil {
				t.Fatal(err)
			}
			for _, handler := range []struct {
				name string
				want bool
			}{{"notify", tc.notify}, {"wait32", tc.wait32}, {"wait64", tc.wait64}} {
				if bytes.Contains(binary, []byte("atomic "+handler.name+" helper arity")) != handler.want {
					t.Errorf("unexpected atomic %s handler linkage", handler.name)
				}
			}
			if bytes.Contains(binary, []byte("atomic helper memory index %d is unavailable")) != tc.indexedMemory64 {
				t.Error("unexpected indexed atomic helper linkage")
			}
			if strings.Contains(names, ".atomicWaitInvocationContexts") != waits ||
				strings.Contains(names, ".memoryWaiters") != waits {
				t.Error("unexpected atomic wait registry linkage")
			}
			if bytes.Contains(binary, []byte("wago: instance closed while waiting")) != waits {
				t.Error("unexpected atomic wait cancellation diagnostic linkage")
			}
		})
	}
}

func TestBuildBareStandaloneReportsMissingFunctionImport(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	// (module (import "env" "f" (func)) (func (export "_start") call 0))
	callsImport := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		2, 9, 1, 3, 'e', 'n', 'v', 1, 'f', 0, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 1,
		10, 6, 1, 4, 0, 0x10, 0, 0x0b,
	}
	// (module (import "env" "f" (func)) (export "_start" (func 0)))
	reexportsImport := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		2, 9, 1, 3, 'e', 'n', 'v', 1, 'f', 0, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0,
	}
	// (module (import "env" "f" (func)) (func (export "_start")) (start 0))
	startsImport := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		2, 9, 1, 3, 'e', 'n', 'v', 1, 'f', 0, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 1,
		8, 1, 0,
		10, 4, 1, 2, 0, 0x0b,
	}
	typedImport := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), []byte{0x60, 4, 0x7f, 0x7e, 0x7d, 0x7c, 0})),
		wasmtest.Section(2, wasmtest.Vec([]byte{3, 'e', 'n', 'v', 1, 'f', 0, 0})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
	)
	typedArgs := []string{"1.5:f32", "0xffffffffffffffff", "-2.5", "3.25"}
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	for _, tc := range []struct {
		name      string
		source    []byte
		tinyGo    bool
		checkArgs bool
		args      []string
	}{
		{name: "local export", source: callsImport},
		{name: "import re-export", source: reexportsImport},
		{name: "imported start", source: startsImport},
		{name: "TinyGo local export", source: callsImport, tinyGo: true},
		{name: "argument validation", source: bytes.Replace(callsImport, []byte("_start"), []byte("invoke"), 1), checkArgs: true},
		{name: "typed arguments", source: typedImport, args: typedArgs},
		{name: "TinyGo typed arguments", source: typedImport, args: typedArgs, tinyGo: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tinyGo {
				if !host.supportsTinyGo() {
					t.Skipf("TinyGo standalone is unsupported on %s", host)
				}
				if _, err := exec.LookPath("tinygo"); err != nil {
					t.Skip("tinygo is not installed")
				}
			}
			dir := t.TempDir()
			input := filepath.Join(dir, "import.wasm")
			if err := os.WriteFile(input, tc.source, 0o644); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, DefaultOutput("import.wasm", host))
			result, err := Build(Request{Input: input, Output: output, Target: host, TinyGo: tc.tinyGo, KeepSymbols: true})
			if err != nil {
				t.Fatal(err)
			}
			message, err := exec.Command(result.Output, tc.args...).CombinedOutput()
			want := filepath.Base(result.Output) + `: module imports "env.f", but nothing provides it: ` + wago.ErrMissingImport.Error()
			if err == nil || strings.TrimSpace(string(message)) != want {
				t.Fatalf("run bare standalone: %v\n%s, want %q", err, message, want)
			}
			if tc.checkArgs {
				message, err := exec.Command(result.Output, "extra").CombinedOutput()
				want := filepath.Base(result.Output) + ": expected 0 arg(s), got 1"
				if err == nil || strings.TrimSpace(string(message)) != want {
					t.Fatalf("validate arguments: %v\n%s, want %q", err, message, want)
				}
			}
			if len(tc.args) != 0 {
				for _, invalid := range []struct {
					args []string
					want string
				}{
					{nil, "expected 4 arg(s), got 0"},
					{[]string{"1", "2:bad", "3", "4"}, `arg 1: bad type suffix in "2:bad"`},
					{[]string{"1", "2", "bad", "4"}, `arg 2 ("bad"): strconv.ParseFloat: parsing "bad": invalid syntax`},
				} {
					output, err := exec.Command(result.Output, invalid.args...).CombinedOutput()
					want := filepath.Base(result.Output) + ": " + invalid.want
					if err == nil || strings.TrimSpace(string(output)) != want {
						t.Fatalf("typed arguments %q: %q, %v, want %q", invalid.args, output, err, want)
					}
				}
			}
			names := assertNoCompilerSymbols(t, result.Output)
			assertDiagnosticLauncher(t, names)
			assertNoPluginRuntime(t, names)
			assertNoFunctionImportRuntime(t, names)
			assertNoFunctionReexportRuntime(t, names)
			assertNoStartRuntime(t, names)
			assertNoMemoryImportRuntime(t, names)
			assertNoTagImportRuntime(t, names)
			assertNoSharedMemoryRuntime(t, names)
			assertNoAtomicWaitRuntime(t, names)
			assertNoNativeCollectorRuntime(t, names)
			assertNoReferenceStoreRuntime(t, names)
			for _, symbol := range []string{
				".resolveInstantiationImports", ".validateImportBindingsWithPluginGC",
				"/wago.pluginPublicGCState", "/wago.gcPublicState",
				"/wago.gcDomainTopology", "/wago.gcObjectTokenIndex",
			} {
				if strings.Contains(names, symbol) {
					t.Errorf("unbound standalone retained %s", symbol)
				}
			}
		})
	}
}

func TestBuildBareStandaloneReportsMissingGlobalImport(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	dir := t.TempDir()
	input := filepath.Join(dir, "global.wasm")
	// (module (import "env" "g" (global i32)) (func (export "_start")))
	source := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		2, 10, 1, 3, 'e', 'n', 'v', 1, 'g', 3, 0x7f, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0,
		10, 4, 1, 2, 0, 0x0b,
	}
	if err := os.WriteFile(input, source, 0o644); err != nil {
		t.Fatal(err)
	}
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	output := filepath.Join(dir, DefaultOutput("global.wasm", host))
	result, err := Build(Request{Input: input, Output: output, Target: host, KeepSymbols: true})
	if err != nil {
		t.Fatal(err)
	}
	message, err := exec.Command(result.Output).CombinedOutput()
	if err == nil || !bytes.Contains(message, []byte(`missing imported global "env.g"`)) {
		t.Fatalf("run bare standalone with a global import: %v\n%s", err, message)
	}
	names := assertNoCompilerSymbols(t, result.Output)
	assertNoFunctionImportRuntime(t, names)
	assertNoFuncrefStorageImportRuntime(t, names)
	assertNoMemoryImportRuntime(t, names)
	assertNoTagImportRuntime(t, names)
	assertNoReferenceStoreRuntime(t, names)
	assertNoInstantiationOptions(t, names)
	if strings.Contains(names, ".detachImportedTables") || strings.Contains(names, ".applyResolvedTablePolicy") {
		t.Fatal("standalone with only an imported global retained imported table handling")
	}
	if strings.Contains(names, ".attachGlobalImports") || strings.Contains(names, ".instantiateWithCleanup") {
		t.Fatal("standalone with an unbound global linked instance construction")
	}
}

func TestBuildBareStandaloneReportsMissingMemoryImport(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	t.Setenv("WAGO_BOUNDS", "explicit")
	memory := []byte{3, 'e', 'n', 'v', 1, 'm', 2, 1, 1, 2}
	table := []byte{3, 'a', '.', 'b', 3, 'c', '.', 'd', 1, 0x70, 0, 1}
	global := []byte{3, 'e', 'n', 'v', 1, 'g', 3, 0x7f, 0}
	for _, tc := range []struct {
		name    string
		imports [][]byte
		want    string
		tinyGo  bool
	}{
		{name: "memory", imports: [][]byte{memory}, want: `missing imported memory "env.m"`},
		{name: "global takes precedence", imports: [][]byte{memory, global}, want: `missing imported global "env.g"`},
		{name: "memory takes precedence", imports: [][]byte{table, memory}, want: `missing imported memory "env.m"`},
		{name: "dotted table import", imports: [][]byte{table}, want: `missing imported table "a.b.c.d"`},
		{name: "TinyGo memory", imports: [][]byte{memory}, want: `missing imported memory "env.m"`, tinyGo: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
			if tc.tinyGo {
				if !target.supportsTinyGo() {
					t.Skipf("TinyGo standalone is unsupported on %s", target)
				}
				if _, err := exec.LookPath("tinygo"); err != nil {
					t.Skip("tinygo is not installed")
				}
			}
			dir := t.TempDir()
			input := filepath.Join(dir, "memory.wasm")
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(2, wasmtest.Vec(tc.imports...)),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("_start", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
			)
			if err := os.WriteFile(input, source, 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := Build(Request{
				Input: input, Output: filepath.Join(dir, DefaultOutput("memory.wasm", target)),
				Target: target, TinyGo: tc.tinyGo, KeepSymbols: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			message, err := exec.Command(result.Output).CombinedOutput()
			want := filepath.Base(result.Output) + ": " + tc.want
			if err == nil || strings.TrimSpace(string(message)) != want {
				t.Fatalf("run bare standalone with storage imports: %v\n%s, want %q", err, message, want)
			}
			names := assertNoCompilerSymbols(t, result.Output)
			assertNoReferenceStoreRuntime(t, names)
			assertNoInstantiationOptions(t, names)
			if strings.Contains(names, "memoryImportAttachments).attach") || strings.Contains(names, ".instantiateWithCleanup") {
				t.Fatal("standalone with an unbound memory linked instance construction")
			}
		})
	}
}

func TestBuildNativeExceptionTagProfiles(t *testing.T) {
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !target.supportsCore3() {
		t.Skipf("Core 3 standalone is unsupported on %s", target)
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	// run throws an i32 payload of 42 and catches it with try_table.
	catch := []byte{10, 20, 1, 18, 0, 2, 0x7f, 0x1f, 0x40, 1, 0, 0, 0, 0x41, 42, 8, 0, 0x0b, 0x41, 0, 0x0b, 0x0b}
	for _, tc := range []struct {
		name     string
		sections []byte
		code     []byte
		trap     string
		imported bool
	}{
		{name: "scalar", sections: []byte{3, 2, 1, 1}, code: []byte{10, 6, 1, 4, 0, 0x41, 42, 0x0b}},
		{name: "local", sections: []byte{3, 2, 1, 1, 13, 3, 1, 0, 0}, code: catch},
		{name: "imported", sections: []byte{2, 10, 1, 3, 'e', 'n', 'v', 1, 't', 4, 0, 0, 3, 2, 1, 1},
			code: catch, trap: `imported tag "env.t" must be an instance-exported *wago.Tag`, imported: true},
		{name: "tagless try_table", sections: []byte{3, 2, 1, 1},
			code: []byte{10, 10, 1, 8, 0, 0x1f, 0x7f, 0, 0x41, 42, 0x0b, 0x0b}},
		// ref.null exn followed by throw_ref.
		{name: "tagless", sections: []byte{3, 2, 1, 1}, code: []byte{10, 7, 1, 5, 0, 0xd0, 0x69, 0x0a, 0x0b}, trap: "null reference"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "tag.wasm")
			source := append([]byte{
				0, 'a', 's', 'm', 1, 0, 0, 0, 1, 9, 2, 0x60, 1, 0x7f, 0, 0x60, 0, 1, 0x7f,
			}, tc.sections...)
			source = append(source, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0)
			source = append(source, tc.code...)
			if err := os.WriteFile(input, source, 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := Build(Request{
				Input: input, Output: filepath.Join(dir, DefaultOutput("tag.wasm", target)),
				Target: target, Core: 3, Invoke: "run", KeepSymbols: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := exec.Command(result.Output).CombinedOutput()
			if tc.trap != "" {
				if err == nil || !strings.Contains(string(got), tc.trap) {
					t.Fatalf("standalone tag fixture: %q, %v, want error %q", got, err, tc.trap)
				}
			} else if err != nil || strings.TrimSpace(string(got)) != "42" {
				t.Fatalf("standalone local tag: %q, %v", got, err)
			}
			names := assertNoCompilerSymbols(t, result.Output)
			if tc.imported {
				assertNoInstantiationOptions(t, names)
				if strings.Contains(names, ".attachTagImports") || strings.Contains(names, ".installNativeTagDirectory") {
					t.Fatal("standalone with an unbound tag linked tag attachment or installation")
				}
			} else {
				assertNoTagImportRuntime(t, names)
			}
			if tc.name == "scalar" || tc.name == "local" || tc.name == "tagless try_table" {
				for _, symbol := range []string{".NewRuntime", ".(*Runtime).AdoptModule", ".(*Runtime).instantiateOrigin", ".(*Module).endUse", ".(*Runtime).registerInstance", ".(*Runtime).unregisterInstance"} {
					if strings.Contains(names, symbol+"\n") {
						t.Errorf("direct standalone linked managed runtime startup %s", symbol)
					}
				}
			}
			if runtime.GOARCH == "riscv64" && !tc.imported {
				if strings.Contains(names, ".rv64ExceptionHandling") != (tc.name != "scalar") {
					t.Fatal("unexpected RV64 exception validation linkage")
				}
			}
			if tc.name == "scalar" {
				if strings.Contains(names, ".selectExecutionFeatures\n") {
					t.Error("standalone retained runtime selection of the baked Core feature mask")
				}
				assertNoGuestMemoryAllocator(t, names)
				if strings.Contains(names, ".referenceLifetime.finalize.func") {
					t.Error("standalone without reference storage retained coordinated finalization callbacks")
				}
				for _, symbol := range []string{".compiledStartIndex", ".rv64ThreadedMemoryProfile", ".validateRV64TableCapacityDirectory", ".compiledLegacyTableWitness", ".(*compiledWriter).functionImports", ".(*RuntimeConfig).WithOptimizations", ".(*RuntimeConfig).Validate", ".(*Instance).Invoke", ".(*Instance).tryInvokeCachedIsolatedNumeric"} {
					if strings.Contains(names, symbol+"\n") {
						t.Errorf("standalone retained admission for an absent resource: %s", symbol)
					}
				}
			}
			if tc.name == "local" {
				assertNoNativeCollectorRuntime(t, names)
				assertNoReferenceStoreRuntime(t, names)
				if strings.Contains(names, "rv64NativeTableTargets).") {
					t.Fatal("scalar exception retained native table target validation")
				}
			}
		})
	}
}

func TestBuildBareStandaloneReportsMissingFuncrefTableImport(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	dir := t.TempDir()
	input := filepath.Join(dir, "table.wasm")
	// (module (import "env" "t" (table 1 funcref)) (func (export "_start")))
	source := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		2, 11, 1, 3, 'e', 'n', 'v', 1, 't', 1, 0x70, 0, 1,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0,
		10, 4, 1, 2, 0, 0x0b,
	}
	if err := os.WriteFile(input, source, 0o644); err != nil {
		t.Fatal(err)
	}
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	output := filepath.Join(dir, DefaultOutput("table.wasm", host))
	result, err := Build(Request{Input: input, Output: output, Target: host, KeepSymbols: true})
	if err != nil {
		t.Fatal(err)
	}
	message, err := exec.Command(result.Output).CombinedOutput()
	if err == nil || !bytes.Contains(message, []byte(`missing imported table "env.t"`)) {
		t.Fatalf("run bare standalone with a funcref table import: %v\n%s", err, message)
	}
	names := assertNoCompilerSymbols(t, result.Output)
	assertNoMemoryImportRuntime(t, names)
	assertNoTagImportRuntime(t, names)
	assertNoSharedMemoryRuntime(t, names)
	assertNoAtomicWaitRuntime(t, names)
	assertNoNativeCollectorRuntime(t, names)
	assertNoInstantiationOptions(t, names)
	if strings.Contains(names, ".retainProducerRootsInImportedTablesMode") || strings.Contains(names, ".instantiateWithCleanup") {
		t.Fatal("standalone with an unbound table linked instance construction")
	}
	for _, symbol := range []string{
		".globalImportBindingKey", ".importedGlobals", ".validateImportedGlobal", ".validateResolvedImportedGlobal",
		".detachImportedGlobals", ".attachGlobalImports", ".retainProducerRootsInImportedGlobalsMode",
		".transferImportedGlobalAttachment", "globalImportAttachments).",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("standalone without global imports retained %s", symbol)
		}
	}
}

func assertNoInstantiationOptions(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".applyInstantiateOptions", ".applyPolicy",
		".resolveInstantiationImports", ".resolveInstanceImports", ".preflightImportBindings",
		".indexDeclaredImportIdentities", "Module).describeImports",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("bare standalone retained %s", symbol)
		}
	}
}

func assertNoSnapshotConstruction(t *testing.T, names string) {
	t.Helper()
	for _, symbol := range []string{
		".snapshotMetadataBytes", ".cloneCompiledMetadataWithDataPolicy", "Compiled).closeNativeActiveData",
		"Compiled).validateArenaFootprint", ".InstantiateArenaNeed",
	} {
		if strings.Contains(names, symbol) {
			t.Errorf("bare standalone retained %s", symbol)
		}
	}
}

func TestBuildNativeModuleWithData(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	// (module (memory 1) (data (i32.const 0) "M")
	//   (func (export "run") (result i32) (i32.load8_u (i32.const 0))))
	singleMemory := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0, 1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0, 5, 3, 1, 0, 1, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0,
		10, 9, 1, 7, 0, 0x41, 0, 0x2d, 0, 0, 0x0b,
		11, 7, 1, 0, 0x41, 0, 0x0b, 1, 'M',
	}
	// (module (memory 1) (memory 1) (data (memory 1) (i32.const 0) "M")
	//   (func (export "run") (result i32) (i32.load8_u 1 (i32.const 0))))
	twoMemories := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0, 1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0, 5, 5, 2, 0, 1, 0, 1, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0,
		10, 10, 1, 8, 0, 0x41, 0, 0x2d, 0x40, 1, 0, 0x0b,
		11, 8, 1, 2, 1, 0x41, 0, 0x0b, 1, 'M',
	}
	// Initialize the table and memory from passive segments, drop the segments,
	// then add the indirect call result (9) to the initialized byte ('!').
	passiveSegments := []byte{
		0, 'a', 's', 'm', 1, 0, 0, 0, 1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 3, 2, 0, 0, 4, 4, 1, 0x70, 0, 1, 5, 3, 1, 0, 1,
		7, 7, 1, 3, 'r', 'u', 'n', 0, 1, 9, 5, 1, 1, 0, 1, 0, 12, 1, 1,
		10, 46, 2, 4, 0, 0x41, 9, 0x0b, 39, 0,
		0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 8, 0, 0, 0xfc, 9, 0,
		0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 12, 0, 0, 0xfc, 13, 0,
		0x41, 0, 0x11, 0, 0, 0x41, 0, 0x2d, 0, 0, 0x6a, 0x0b,
		11, 4, 1, 1, 1, '!',
	}
	// The build helper can back this compact initializer with a native arena.
	// Read the last byte so the executable must retain the complete payload.
	const segmentSize = 2048
	segments := make([][]byte, 64)
	for i := range segments {
		segment := append([]byte{0, 0x41}, wasmtest.SLEB32(int32(i*segmentSize))...)
		segment = append(segment, 0x0b)
		segment = append(segment, wasmtest.ULEB(segmentSize)...)
		segments[i] = append(segment, bytes.Repeat([]byte{'M'}, segmentSize)...)
	}
	lastByte := int32(len(segments)*segmentSize - 1)
	loadLast := append([]byte{0x41}, wasmtest.SLEB32(lastByte)...)
	loadLast = append(loadLast, 0x2d, 0, 0, 0x0b)
	compactData := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(loadLast))),
		wasmtest.Section(11, wasmtest.Vec(segments...)),
	)
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	for _, tc := range []struct {
		name     string
		source   []byte
		features wago.CoreFeatures
		want     string
	}{
		{name: "core1", source: singleMemory, want: "77"},
		{name: "core2", source: singleMemory, features: wago.CoreFeaturesV2, want: "77"},
		{name: "compact active data", source: compactData, want: "77"},
		{name: "secondary memory", source: twoMemories, features: wago.CoreFeaturesV2 | wago.CoreFeatureMultiMemory, want: "77"},
		{name: "passive segments", source: passiveSegments, features: wago.CoreFeaturesV2, want: "42"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := wago.NewRuntimeConfig().WithCoreFeatures(tc.features).Validate(); err != nil {
				t.Skipf("requires %s: %v", tc.features, err)
			}
			dir := t.TempDir()
			input := filepath.Join(dir, "data.wasm")
			if err := os.WriteFile(input, tc.source, 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := Build(Request{
				Input: input, Output: filepath.Join(dir, DefaultOutput("data.wasm", target)), Target: target,
				Invoke: "run", Features: tc.features, FeaturesSet: true, KeepSymbols: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(result.Output).CombinedOutput()
			if err != nil || strings.TrimSpace(string(output)) != tc.want {
				t.Fatalf("native data initialization: %q, %v", output, err)
			}
			names := assertNoCompilerSymbols(t, result.Output)
			assertNoSnapshotConstruction(t, names)
			if strings.Contains(names, ".preflightCore1DataSegments") != (tc.features == 0) {
				t.Fatal("unexpected Core 1 data preflight linkage")
			}
			if strings.Contains(names, ".instantiateSecondaryMemories") != tc.features.IsEnabled(wago.CoreFeatureMultiMemory) {
				t.Fatal("unexpected secondary-memory linkage")
			}
			assertNoGuardedGrowthRuntime(t, names)
			assertNoSharedMemoryRuntime(t, names)
			assertNoDynamicSegmentOffsets(t, names)
			assertConstExprFeatureScanner(t, names, false)
			assertNoMemoryImportRuntime(t, names)
		})
	}
}

func TestBuildStandaloneRV64IndirectTail(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "riscv64" {
		t.Skip("requires the Linux RISC-V native tail-call backend")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")

	// An exported table can change, so the indirect tail needs a runtime
	// dispatcher even though its initial target is local.
	targetType := []byte{0x60, 1, 0x7f, 1, 0x7f}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(targetType, []byte{0x60, 2, 0x7f, 0x7f, 1, 0x7f}, targetType)),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(4, []byte{1, 0x70, 1, 3, 3}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0),
			wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("table", 1, 0))),
		wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 2, 0, 1}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x13, 2, 0, 0x0b}))),
	)
	dir := t.TempDir()
	input := filepath.Join(dir, "tail.wasm")
	if err := os.WriteFile(input, module, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Build(Request{
		Input: input, Output: filepath.Join(dir, "tail"), KeepSymbols: true,
		Target: Target{OS: runtime.GOOS, Arch: runtime.GOARCH}, Invoke: "run",
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(result.Output, "42", "0").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "42" {
		t.Fatalf("standalone indirect tail call: %q, %v", output, err)
	}
	names := assertNoCompilerSymbols(t, result.Output)
	assertNoPluginRuntime(t, names)
	if !strings.Contains(names, "riscv64.newNativeTailDispatcher") {
		t.Fatal("indirect tail fixture needs the native dispatcher")
	}
}

func TestBuildNativeModuleWithLocalTable(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	request := Request{
		Input:  filepath.Join(root, "tests", "fixtures", "wasm", "indirect.wasm"),
		Output: filepath.Join(t.TempDir(), DefaultOutput("indirect.wasm", target)),
		Target: target, Invoke: "apply", KeepSymbols: true, FeaturesSet: true,
	}
	result, err := Build(request)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(result.Output, "2", "3", "5").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "15" {
		t.Fatalf("local table call: %q, %v", output, err)
	}
	names := assertNoCompilerSymbols(t, result.Output)
	assertNoModuleImportRuntime(t, names)
	assertNoNativeCollectorRuntime(t, names)
	assertNoDynamicSegmentOffsets(t, names)
	assertConstExprFeatureScanner(t, names, false)
	if !strings.Contains(names, ".preflightCore1ElementSegments") || strings.Contains(names, ".preflightCore1DataSegments") {
		t.Fatal("Core 1 fixture needs element preflight only")
	}
	if !strings.Contains(names, "referenceStore).registerInstance") {
		t.Fatal("local funcref table omitted reference registration")
	}
	for _, symbol := range []string{".NewRuntime", ".(*Runtime).AdoptModule", ".(*Runtime).instantiateOrigin", ".(*Module).endUse", ".(*Runtime).registerInstance", ".(*Runtime).unregisterInstance"} {
		if strings.Contains(names, symbol+"\n") {
			t.Errorf("single-image funcref table retained managed runtime startup %s", symbol)
		}
	}
	if !strings.Contains(names, "referenceStore).registerStructuralCallKeysLocked") {
		t.Fatal("local funcref table omitted structural call key registration")
	}
	if strings.Contains(names, ".rv64DeclaredGraph") || strings.Contains(names, ".rv64ValidatePrimitiveFunctionKeys") {
		t.Fatal("flat scalar table retained typed graph key validation")
	}
	for _, writer := range []string{"nameSec", "globalImports", "globals", "compiledData", "passiveData", "memories", "tags"} {
		if strings.Contains(names, "compiledWriter)."+writer) {
			t.Errorf("local table fixture retained metadata writer %s", writer)
		}
	}
	if strings.Contains(names, "compiler/optimization.") {
		t.Fatal("local funcref table retained the compiler optimization catalog")
	}
	if strings.Contains(names, ".rv64MutableFuncrefGlobal") {
		t.Fatal("local funcref table retained mutable funcref global admission")
	}
	if strings.Contains(names, ".rv64ValidateReferenceFunctions") {
		t.Fatal("local funcref table retained reference-function admission")
	}
	if strings.Contains(names, ".rv64PrepareTailTargets") || strings.Contains(names, ".rv64ValidateTailTargets") {
		t.Fatal("local funcref table retained native tail target directories")
	}
	if strings.Contains(names, ".rv64GlobalDeclarationError") || strings.Contains(names, ".rv64GlobalImportError") {
		t.Fatal("local funcref table retained global admission")
	}
	for _, witness := range []string{".rv64Core3RefDeclarationMetadata", ".rv64Core3RefIsNullExecutionCodecPrefix", ".rv64StateOnlyExternrefGlobals"} {
		if strings.Contains(names, witness) {
			t.Errorf("local funcref table retained %s", witness)
		}
	}
	for _, symbol := range []string{".rv64FunctionDependencyGraph", "(*dependencyGraph).", ".dependencyReceipt.", ").prepareHostReentryState"} {
		if strings.Contains(names, symbol) {
			t.Errorf("local funcref table retained %s", symbol)
		}
	}
	if strings.Contains(names, ").marshalPublicReferenceArgs") || strings.Contains(names, ").translatePublicReferenceResultsMode") {
		t.Fatal("scalar launcher retained public reference argument or result conversion")
	}
	for _, boundary := range []string{".rv64PublicHostReferenceBoundary", ".rv64PublicDynamicHostReferenceBoundary", ".rv64PublicReferenceCallBoundary"} {
		if strings.Contains(names, boundary) {
			t.Errorf("local funcref table retained %s", boundary)
		}
	}
	if strings.Contains(names, ".validateTableDeclarations") || strings.Contains(names, ".validateTableBindings") {
		t.Fatal("local table retained structural table validation")
	}
	if strings.Contains(names, ".validateGCConstExpr") || strings.Contains(names, ".evalCompiledGCConstExpr") {
		t.Fatal("local funcref table retained the GC expression path")
	}

	t.Run("TinyGo", func(t *testing.T) {
		if !target.supportsTinyGo() {
			t.Skipf("TinyGo standalone is unsupported on %s", target)
		}
		if _, err := exec.LookPath("tinygo"); err != nil {
			t.Skip("tinygo is not installed")
		}
		request.TinyGo = true
		request.Output = filepath.Join(t.TempDir(), DefaultOutput("indirect.wasm", target))
		result, err := Build(request)
		if err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(result.Output, "2", "3", "5").CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != "15" {
			t.Fatalf("TinyGo standalone local table call: %q, %v", output, err)
		}
	})
}

func TestBuildNativeModuleWithDynamicSegmentOffsets(t *testing.T) {
	features := wago.CoreFeaturesV2 | wago.CoreFeatureExtendedConst | wago.CoreFeatureExtendedConstExpressions
	if err := wago.NewRuntimeConfig().WithCoreFeatures(features).Validate(); err != nil {
		t.Skipf("requires extended constant expressions: %v", err)
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	for _, tc := range []struct {
		name       string
		offset     []byte
		index      byte
		arithmetic bool
	}{
		{"global", []byte{0x23, 0, 0x0b}, 1, false},
		{"expression", []byte{0x23, 0, 0x41, 1, 0x6a, 0x0b}, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "offsets.wasm")
			// Both segments use the same offset. run adds the indirect call (9)
			// and the byte loaded from memory ('M').
			source := []byte{
				0, 'a', 's', 'm', 1, 0, 0, 0, 1, 5, 1, 0x60, 0, 1, 0x7f,
				3, 3, 2, 0, 0, 4, 4, 1, 0x70, 0, 3, 5, 3, 1, 0, 1,
				6, 6, 1, 0x7f, 0, 0x41, 1, 0x0b, 7, 7, 1, 3, 'r', 'u', 'n', 0, 1,
				9, byte(4 + len(tc.offset)), 1, 0,
			}
			source = append(source, tc.offset...)
			source = append(source, 1, 0, 10, 20, 2, 4, 0, 0x41, 9, 0x0b, 13, 0,
				0x41, tc.index, 0x11, 0, 0, 0x41, tc.index, 0x2d, 0, 0, 0x6a, 0x0b,
				11, byte(4+len(tc.offset)), 1, 0)
			source = append(source, tc.offset...)
			source = append(source, 1, 'M')
			if err := os.WriteFile(input, source, 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := Build(Request{
				Input: input, Output: filepath.Join(dir, DefaultOutput("offsets.wasm", target)), Target: target,
				Invoke: "run", Features: features, FeaturesSet: true, KeepSymbols: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := exec.Command(result.Output).CombinedOutput()
			if err != nil || strings.TrimSpace(string(got)) != "86" {
				t.Fatalf("standalone segment offsets: %q, %v", got, err)
			}
			names := assertNoCompilerSymbols(t, result.Output)
			if runtime.GOARCH != "riscv64" {
				assertConstExprFeatureScanner(t, names, false)
			}
			if !strings.Contains(names, ".activeSegmentOffset") ||
				strings.Contains(names, ".evalCompiledScalarConstExpr") != tc.arithmetic ||
				strings.Contains(names, ".validateDeferredOffsetGlobal") {
				t.Fatal("unexpected standalone segment offset linkage")
			}
		})
	}
}

func TestBuildNativeModuleWithTableInitializer(t *testing.T) {
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !target.supportsCore3() || target.Arch == "riscv64" {
		t.Skip("requires general native typed function references")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	initializer := []byte{0x40, 0, 0x70, 0, 3, 0xd2, 1, 0x0b}
	for _, layout := range []struct {
		name    string
		tables  [][]byte
		index   byte
		element []byte
		globals [][]byte
	}{
		{"single", [][]byte{initializer}, 0, []byte{0, 0x41, 1, 0x0b, 1, 2}, nil},
		{"multiple", [][]byte{{0x70, 0, 1}, initializer}, 1, []byte{2, 1, 0x41, 1, 0x0b, 0, 1, 2}, nil},
		{"global", [][]byte{initializer}, 0, []byte{4, 0x41, 1, 0x0b, 1, 0x23, 0, 0x0b}, [][]byte{{0x70, 0, 0xd2, 2, 0x0b}}},
	} {
		t.Run(layout.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "table-init.wasm")
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 1, 0x7f, 1, 0x7f}, []byte{0x60, 0, 1, 0x7f})),
				wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{1})),
				wasmtest.Section(4, wasmtest.Vec(layout.tables...)),
				wasmtest.Section(6, wasmtest.Vec(layout.globals...)),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 0), wasmtest.ExportEntry("override", 0, 2))),
				// Override just the middle slot so the two targets require table dispatch.
				wasmtest.Section(9, wasmtest.Vec(layout.element)),
				wasmtest.Section(10, wasmtest.Vec(
					wasmtest.Code([]byte{0x20, 0, 0x11, 1, layout.index, 0x0b}),
					wasmtest.Code([]byte{0x41, 42, 0x0b}),
					wasmtest.Code([]byte{0x41, 7, 0x0b}),
				)),
			)
			if err := os.WriteFile(input, source, 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := Build(Request{
				Input: input, Output: filepath.Join(dir, DefaultOutput("table-init.wasm", target)),
				Target: target, Core: 3, Invoke: "callAt",
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct{ slot, want string }{{"0", "42"}, {"1", "7"}, {"2", "42"}} {
				got, err := exec.Command(result.Output, tc.slot).CombinedOutput()
				if err != nil || strings.TrimSpace(string(got)) != tc.want {
					t.Fatalf("table slot %s: %q, %v, want %s", tc.slot, got, err, tc.want)
				}
			}
		})
	}
}

func TestBuildNativeModuleWithElementExpression(t *testing.T) {
	target := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !target.supportsCore3() || target.Arch == "riscv64" {
		t.Skip("requires general native GC support")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	for _, tc := range []struct {
		name     string
		table    []byte
		typ      []byte
		sections []byte
	}{
		// (elem (i32.const 0) anyref (struct.new 0 (i32.const 42)))
		{"active", nil, nil, []byte{
			9, 14, 1, 6, 0, 0x41, 0, 0x0b, 0x6e, 1, 0x41, 42, 0xfb, 0, 0, 0x0b,
			10, 15, 1, 13, 0, 0x41, 0, 0x25, 0, 0xfb, 0x16, 0, 0xfb, 2, 0, 0, 0x0b,
		}},
		// (elem anyref (struct.new 0 (i32.const 42))), copied with table.init.
		{"passive", nil, nil, []byte{
			9, 10, 1, 5, 0x6e, 1, 0x41, 42, 0xfb, 0, 0, 0x0b,
			10, 25, 1, 23, 0, 0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 12, 0, 0,
			0x41, 0, 0x25, 0, 0xfb, 0x16, 0, 0xfb, 2, 0, 0, 0x0b,
		}},
		// The initializer allocates once. Reading slot 1 exercises the repeated entry.
		{"repeated", []byte{4, 12, 1, 0x40, 0, 0x6e, 0, 2, 0x41, 42, 0xfb, 0, 0, 0x0b}, nil, []byte{
			10, 15, 1, 13, 0, 0x41, 1, 0x25, 0, 0xfb, 0x16, 0, 0xfb, 2, 0, 0, 0x0b,
		}},
		// Build an array in an active segment and read its first element.
		{"array", nil, []byte{0x5e, 0x7f, 1}, []byte{
			9, 15, 1, 6, 0, 0x41, 0, 0x0b, 0x6e, 1, 0x41, 42, 0xfb, 8, 0, 1, 0x0b,
			10, 16, 1, 14, 0, 0x41, 0, 0x25, 0, 0xfb, 0x16, 0, 0x41, 0, 0xfb, 11, 0, 0x0b,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "element.wasm")
			aggregate := tc.typ
			if aggregate == nil {
				aggregate = []byte{0x5f, 1, 0x7f, 0}
			}
			table := tc.table
			if table == nil {
				table = []byte{4, 4, 1, 0x6e, 0, 1}
			}
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(aggregate, []byte{0x60, 0, 1, 0x7f})),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				table,
			)
			source = append(source, 7, 7, 1, 3, 'r', 'u', 'n', 0, 0)
			source = append(source, tc.sections...)
			if err := os.WriteFile(input, source, 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := Build(Request{
				Input: input, Output: filepath.Join(dir, DefaultOutput("element.wasm", target)), Target: target,
				Core: 3, Invoke: "run", KeepSymbols: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := exec.Command(result.Output).CombinedOutput()
			if err != nil || strings.TrimSpace(string(got)) != "42" {
				t.Fatalf("standalone element expression: %q, %v", got, err)
			}
			names := assertNoCompilerSymbols(t, result.Output)
			if runtime.GOARCH != "riscv64" {
				assertConstExprFeatureScanner(t, names, false)
				if strings.Contains(names, ".validateGCConstExpr") {
					t.Fatal("standalone element expression retained structural expression validation")
				}
			}
			if !strings.Contains(names, ".evalCompiledGCConstExpr") {
				t.Fatal("standalone element expression omitted its evaluator")
			}
		})
	}
}

func TestBuildNativeModuleWithGCReferences(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !host.supportsCore3() || host.Arch == "riscv64" {
		t.Skip("requires general native GC support")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	output := filepath.Join(t.TempDir(), "gc")
	result, err := Build(Request{Input: filepath.Join(root, "tests/fixtures/wasm/gc-allocation-mix.wasm"), Output: output, Target: host, Core: 3, Invoke: "structs", KeepSymbols: true, FunctionWorkers: 2})
	if err != nil {
		t.Fatal(err)
	}
	names := assertNoCompilerSymbols(t, result.Output)
	if !strings.Contains(names, "/gc/native.NewCollector") {
		t.Fatal("standalone GC allocation omitted the native collector")
	}
	if !strings.Contains(names, "Instance).dispatchGCArrayHelperLocked") {
		t.Fatal("standalone with array instructions omitted array helpers")
	}
	if strings.Contains(names, "gcExternConversionState") {
		t.Fatal("standalone GC allocation retained staged extern conversion")
	}
	got, err := exec.Command(result.Output, "1000").CombinedOutput()
	if err != nil || strings.TrimSpace(string(got)) != "499500" {
		t.Fatalf("GC standalone: %s, %v", got, err)
	}
}

func TestBuildStandaloneGCHelperFamilies(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !host.supportsCore3() || host.Arch == "riscv64" {
		t.Skip("requires general native GC support")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	const wideFields = 70
	wideType := append([]byte{0x5f}, wasmtest.ULEB(wideFields)...)
	wideBody := []byte{0} // no locals
	for range wideFields {
		wideType = append(wideType, 0x7f, 0)
		wideBody = append(wideBody, 0x20, 0) // local.get 0
	}
	wideBody = append(wideBody, 0xfb, 0, 0, 0xfb, 2, 0, wideFields-1, 0x0b)
	type aggregateCase struct {
		name, family, argument string
		typ, table, body       []byte
		wantCopy               bool
	}
	for _, tc := range []aggregateCase{
		{
			name: "struct", family: "struct", argument: "42",
			typ: []byte{0x5f, 1, 0x7f, 0},
			body: []byte{
				0,       // no locals
				0x20, 0, // local.get 0
				0xfb, 0, 0, // struct.new 0
				0xfb, 2, 0, 0, // struct.get 0 0
				0x0b,
			},
		},
		{
			name: "wide-struct", family: "struct", argument: "42",
			typ: wideType, body: wideBody,
		},
		{
			name: "table-set", family: "struct", argument: "42",
			typ:   []byte{0x5f, 1, 0x7f, 1},
			table: []byte{0x63, 0, 1, 1, 1}, // one bounded nullable struct slot
			body: []byte{
				0,                            // no locals
				0x41, 0, 0x20, 0, 0xfb, 0, 0, // index 0; struct.new 0 from the argument
				0x26, 0, // table.set 0
				0x41, 0, 0x25, 0, 0xfb, 2, 0, 0, // read the stored struct field
				0x0b,
			},
		},
		{
			name: "array", family: "array", argument: "42",
			typ: []byte{0x5e, 0x7f, 1},
			body: []byte{
				0,                // no locals
				0x20, 0, 0x41, 4, // local.get 0; i32.const 4
				0xfb, 6, 0, // array.new 0
				0x41, 3, 0xfb, 11, 0, // i32.const 3; array.get 0
				0x0b,
			},
		},
		{
			name: "array-copy", family: "array", argument: "2", wantCopy: true,
			typ: []byte{0x5e, 0x7f, 1},
			body: []byte{
				1, 1, 0x63, 0, // one (ref null 0) local
				0x41, 0, 0x41, 4, 0xfb, 6, 0, // destination array filled with 0
				0x22, 1, 0x41, 0, // local.tee 1; destination offset
				0x41, 42, 0x41, 4, 0xfb, 6, 0, // source array filled with 42
				0x41, 0, 0x20, 0, 0xfb, 17, 0, 0, // source offset; dynamic count; array.copy
				0x20, 1, 0x41, 0, 0xfb, 11, 0, 0x0b, // return destination element 0
			},
		},
	} {
		dir := t.TempDir()
		input := filepath.Join(dir, tc.name+".wasm")
		sections := [][]byte{
			wasmtest.Section(1, wasmtest.Vec(tc.typ, []byte{0x60, 1, 0x7f, 1, 0x7f})),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		}
		if len(tc.table) != 0 {
			sections = append(sections, wasmtest.Section(4, wasmtest.Vec(tc.table)))
		}
		module := wasmtest.Module(append(sections,
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(tc.body))), tc.body...))),
		)...)
		if err := os.WriteFile(input, module, 0o644); err != nil {
			t.Fatal(err)
		}
		for _, toolchain := range []string{"go", "tinygo"} {
			t.Run(tc.name+"/"+toolchain, func(t *testing.T) {
				tinygo := toolchain == "tinygo"
				if tinygo {
					if !host.supportsTinyGo() {
						t.Skipf("TinyGo standalone is unsupported on %s", host)
					}
					if _, err := exec.LookPath("tinygo"); err != nil {
						t.Skip("tinygo is not installed")
					}
				}
				result, err := Build(Request{
					Input: input, Output: filepath.Join(dir, toolchain), Target: host,
					Core: 3, Invoke: "run", KeepSymbols: true, TinyGo: tinygo,
				})
				if err != nil {
					t.Fatal(err)
				}
				names := assertNoCompilerSymbols(t, result.Output)
				image, err := os.ReadFile(result.Output)
				if err != nil {
					t.Fatal(err)
				}
				for _, symbol := range []string{
					"dispatchParkedHost", "rootGCHostArguments", "pushGCHostActivation",
					"dispatchForeignControl", "dispatchHostCall", "hostControlInstances", "registeredHostCtrlFrames",
				} {
					if strings.Contains(names, symbol) {
						t.Errorf("standalone retained unused host boundary symbol %s", symbol)
					}
				}
				for _, diagnostic := range []string{
					"host control extension has invalid magic",
					"host control frame has invalid extended capacity",
				} {
					if bytes.Contains(image, []byte(diagnostic)) != (tc.name == "wide-struct") {
						t.Errorf("unexpected extended host frame diagnostic %q", diagnostic)
					}
				}
				// The dispatcher's diagnostic data survives inlining.
				for _, family := range []struct {
					name, dispatcher, reservation string
					diagnostics                   []string
				}{
					{"struct", ".dispatchGCStructHelperLocked", ".ReserveDeadStructAllocationsWithPoll",
						[]string{"gc struct alloc helper arity", "gc struct dead reservation helper arity", "gc struct get helper arity"}},
					{"array", ".dispatchGCArrayHelperLocked", ".ReserveDeadArrayAllocationsWithPoll",
						[]string{"gc array alloc-uniform helper arity", "gc array get helper arity"}},
				} {
					want := family.name == tc.family
					actual := slices.ContainsFunc(family.diagnostics, func(diagnostic string) bool {
						return bytes.Contains(image, []byte(diagnostic))
					})
					if actual != want {
						t.Errorf("linked %s helpers = %v, want %v", family.name, actual, want)
					}
					if !want && (strings.Contains(names, family.dispatcher) || strings.Contains(names, family.reservation)) {
						t.Errorf("standalone retained unused %s helper symbols", family.name)
					}
				}
				for _, diagnostic := range []string{
					"gc array batch reservation arity", "gc struct batch reservation arity",
					"gc array init-elem helper arity", "gc array init-data helper arity", "gc final cast/array.len helper arity",
					"gc table fill helper arity", "gc table grow helper arity", "gc table init helper arity",
					"gc table copy helper arity", "gc table elem-drop helper arity",
					"function ref.test helper arity", "invalid owned exception helper boundary", "owned exception tag identity",
				} {
					if bytes.Contains(image, []byte(diagnostic)) {
						t.Errorf("standalone retained unused helper diagnostic %q", diagnostic)
					}
				}
				if actual := bytes.Contains(image, []byte("gc array copy helper arity")); actual != tc.wantCopy {
					t.Errorf("linked array.copy helper = %v, want %v", actual, tc.wantCopy)
				}
				if actual, want := bytes.Contains(image, []byte("gc ref.test table-set helper args")), len(tc.table) != 0; actual != want {
					t.Errorf("linked collector table.set helper = %v, want %v", actual, want)
				}
				output, err := exec.Command(result.Output, tc.argument).CombinedOutput()
				if err != nil || strings.TrimSpace(string(output)) != "42" {
					t.Fatalf("GC standalone: %q, %v, want 42", output, err)
				}
			})
		}
	}
}

func TestBuildStandaloneStagedGCExternConversion(t *testing.T) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !host.supportsCore3() || host.Arch == "riscv64" {
		t.Skip("requires staged GC extern conversion")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", root)
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "1")
	dir := t.TempDir()
	input := filepath.Join(dir, "extern-conversion.wasm")
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, []byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0xfb, 1, 0, // struct.new_default 0
			0xfb, 0x1b, // extern.convert_any
			0xd1, 0x0b, // ref.is_null; end
		}))),
	)
	if err := os.WriteFile(input, source, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, toolchain := range []string{"go", "tinygo"} {
		t.Run(toolchain, func(t *testing.T) {
			tinygo := toolchain == "tinygo"
			if tinygo {
				if !host.supportsTinyGo() {
					t.Skipf("TinyGo standalone is unsupported on %s", host)
				}
				if _, err := exec.LookPath("tinygo"); err != nil {
					t.Skip("tinygo is not installed")
				}
			}
			result, err := Build(Request{
				Input: input, Output: filepath.Join(dir, toolchain), Target: host,
				Core: 3, Invoke: "run", KeepSymbols: true, TinyGo: tinygo,
			})
			if err != nil {
				t.Fatal(err)
			}
			names := assertNoCompilerSymbols(t, result.Output)
			if !strings.Contains(names, "gcExternConversionState).externFromAny") {
				t.Fatal("standalone omitted staged extern conversion")
			}
			output, err := exec.Command(result.Output).CombinedOutput()
			if err != nil || strings.TrimSpace(string(output)) != "0" {
				t.Fatalf("GC extern conversion standalone: %q, %v, want 0", output, err)
			}
		})
	}
}
