package run

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
	mod := mustLoadModule(path, config, rt, artifactcache.Cache{})
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
	if got := mustResolveExport(mustLoadModule(compiledPath, config, rt, artifactcache.Cache{}).Compiled(), "f"); got != "f" {
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
			artifact := []byte{'W', 'A', 'G', 'O', 1, 2, 1}
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
	args := mustParseArgs([]string{"7", "1.5:f32"}, []wago.ValType{wago.ValI32, wago.ValI64})
	if got := format("f", args, []uint64{wago.I64(9)}, []wago.ValType{wago.ValI32, wago.ValF32}, []wago.ValType{wago.ValI64}); got != "f(7, 1.5) = 9" {
		t.Fatalf("format result = %q", got)
	}
	if got := format("g", nil, nil, nil, nil); got != "g() = ()" {
		t.Fatalf("format void = %q", got)
	}
	if got := trapReason(&wago.TrapError{Code: wago.TrapDivZero}); got != "integer division by zero" {
		t.Fatalf("typed trap reason = %q", got)
	}
	if got := trapReason(errors.New("plain error")); got != "plain error" {
		t.Fatalf("plain trap reason = %q", got)
	}
}
