package run

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/settings"
	"github.com/wago-org/wago/cli/runtime/internal/artifactcache"
)

type testEnvironment struct{}

func (testEnvironment) ProfileFlags() []command.Flag {
	return []command.Flag{{Name: "plugin", Arg: "<name>"}, {Name: "plugins", Arg: "<names>"}}
}
func (testEnvironment) LoadRuntime(cfg *wago.RuntimeConfig, _ string) *wago.Runtime {
	return wago.NewRuntime(wago.WithRuntimeConfig(cfg))
}
func (testEnvironment) ArtifactCache() artifactcache.Cache { return artifactcache.Cache{} }

func TestPluginListAcceptsSingularAndPluralFlags(t *testing.T) {
	ctx := command.NewContext(nil, map[string]string{"plugin": "wasi", "plugins": "metrics, log"}, nil)
	if got := PluginList(ctx); got != "wasi,metrics, log" {
		t.Fatalf("plugin list = %q", got)
	}
}

func TestOptimizationFlags(t *testing.T) {
	knobs := wago.OptKnobs()
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
	t.Cleanup(func() {
		for _, knob := range knobs {
			wago.SetOptKnob(knob.Name, knob.On)
		}
	})
	name := knobs[0].Name
	ApplyOptimizationFlags(command.NewContext(nil, nil, map[string]bool{name: true}))
	if !wago.OptKnobs()[0].On {
		t.Fatalf("--%s did not enable knob", name)
	}
	ApplyOptimizationFlags(command.NewContext(nil, nil, map[string]bool{"no-" + name: true}))
	if wago.OptKnobs()[0].On {
		t.Fatalf("--no-%s did not disable knob", name)
	}
}

func TestHelpCollapsesBooleanPairs(t *testing.T) {
	var output strings.Builder
	Command(testEnvironment{}).PrintHelp(&output, "wago run")
	text := output.String()
	if !strings.Contains(text, "--<no->st-flags") ||
		strings.Contains(text, "enable: keep comparison results") {
		t.Fatalf("run help did not collapse optimization pair:\n%s", text)
	}
	if !strings.Contains(text, "--parallel, -p [workers]") ||
		!strings.Contains(text, "-p8 / -p 8 / --parallel=8") {
		t.Fatalf("run help did not document function parallelism:\n%s", text)
	}
	pluginIndex := strings.Index(text, "--plugin <name>")
	helpIndex := strings.Index(text, "--help, -h")
	deferredIndex := strings.Index(text, "--<no->deferred-bounds-checking")
	optimizationFlags := OptimizationFlags()
	lastKnobIndex := strings.Index(text, "--<no->"+optimizationFlags[len(optimizationFlags)-2].Name)
	if pluginIndex < 0 || helpIndex < pluginIndex || deferredIndex < helpIndex || lastKnobIndex < deferredIndex {
		t.Fatalf("optimization knobs are not the trailing flag group:\n%s", text)
	}
}

func TestHelpRecognitionAfterSeparatedParallelism(t *testing.T) {
	cmd := Command(testEnvironment{})
	normalized, err := cmd.Normalize([]string{"-p", "8", "--help"})
	if err != nil || !command.WantsHelp(normalized, cmd.PassThrough, cmd.Flags) {
		t.Fatalf("help after separated parallelism was missed: normalized=%v err=%v", normalized, err)
	}
}

func TestFriendlyInstantiationErrorIsProviderNeutral(t *testing.T) {
	for _, importName := range []string{"wasi_snapshot_preview1.fd_write", "acme_host.render"} {
		err := fmt.Errorf(`module imports %q, but nothing provides it: %w`, importName, wago.ErrMissingImport)
		got := friendlyInstantiationError(err).Error()
		if !strings.Contains(got, importName) ||
			!strings.Contains(got, "Add a plugin that provides it.") {
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
		wantPlugin     string
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
		{name: "after separated plugin", args: []string{"--plugin", "wasi", "--parallel=4", "module.wasm"}, wantParallel: "4", wantPlugin: "wasi"},
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
			if got := ctx.Str("plugin"); got != tc.wantPlugin {
				t.Fatalf("plugin = %q, want %q (normalized %v)", got, tc.wantPlugin, args)
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
	if ctx.Str("parallel") != "" || len(ctx.Args) != 2 || ctx.Args[1] != "-p8" {
		t.Fatalf("guest -p8 was consumed: parallel=%q args=%v", ctx.Str("parallel"), ctx.Args)
	}
}

func TestRunConfigCoreVersion(t *testing.T) {
	defaultConfig, err := Config("", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if defaultConfig.CoreFeatures().IsEnabled(wago.CoreFeatureGC) {
		t.Fatal("default run config unexpectedly enabled Core 3 GC")
	}
	core3, err := Config("3", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if core3.CoreFeatures() != wago.CoreFeaturesV3 {
		t.Fatalf("--core 3 features = %v, want %v", core3.CoreFeatures(), wago.CoreFeaturesV3)
	}
	if _, err := Config("4", true, ""); err == nil {
		t.Fatal("unsupported core version accepted")
	}
}

func TestRunConfigParallelism(t *testing.T) {
	for _, tc := range []struct {
		parallel string
		want     int
	}{
		{"", 1},
		{"auto", 0},
		{"0", 0},
		{"1", 1},
		{"8", 8},
	} {
		cfg, err := Config("", true, tc.parallel)
		if err != nil {
			t.Fatalf("parallel %q: %v", tc.parallel, err)
		}
		if got := cfg.FunctionWorkers(); got != tc.want {
			t.Fatalf("parallel %q workers = %d, want %d", tc.parallel, got, tc.want)
		}
	}
	for _, value := range []string{"-1", "many"} {
		if _, err := Config("", true, value); err == nil {
			t.Fatalf("parallel %q accepted", value)
		}
	}
	cfg, err := Config("", false, "8")
	if err != nil || cfg.DeferBoundsChecks() {
		t.Fatalf("combined config = %v, %v", cfg, err)
	}
}

func TestDeferredBoundsCheckingFlags(t *testing.T) {
	cmd := Command(testEnvironment{})
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, true},
		{[]string{"--deferred-bounds-checking"}, true},
		{[]string{"--no-deferred-bounds-checking"}, false},
	} {
		ctx, err := cmd.Parse("wago run", tc.args)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DeferredBoundsChecking(ctx, true)
		if err != nil || got != tc.want {
			t.Fatalf("DeferredBoundsChecking(%v) = %v, %v; want %v", tc.args, got, err, tc.want)
		}
	}
	ctx, err := cmd.Parse("wago run", []string{"--deferred-bounds-checking", "--no-deferred-bounds-checking"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DeferredBoundsChecking(ctx, true); err == nil {
		t.Fatal("conflicting deferred bounds checking flags accepted")
	}
}

func TestDeferredBoundsCheckingUsesConfiguredDefaultAndCLIWins(t *testing.T) {
	if got, err := DeferredBoundsChecking(command.NewContext(nil, nil, nil), false); err != nil || got {
		t.Fatalf("configured default = %v, %v; want false", got, err)
	}
	if got, err := DeferredBoundsChecking(command.NewContext(nil, nil, map[string]bool{deferredBoundsCheckingFlag: true}), false); err != nil || !got {
		t.Fatalf("explicit enable = %v, %v; want true", got, err)
	}
}

func TestConfiguredFeaturesAndOptimizationsMatchRuntimeCatalog(t *testing.T) {
	defaults := settings.Default()
	config := ApplyFeatureDefaults(wago.NewRuntimeConfig(), defaults, true)
	if config.CoreFeatures() != wago.NewRuntimeConfig().CoreFeatures() {
		t.Fatalf("configured features = %s, want %s", config.CoreFeatures(), wago.NewRuntimeConfig().CoreFeatures())
	}
	known := map[string]bool{}
	for _, setting := range settings.Optimizations() {
		known[strings.TrimPrefix(setting.Key, "optimizations.")] = true
	}
	for _, knob := range wago.OptKnobs() {
		if !known[knob.Name] {
			t.Fatalf("runtime knob %q is missing from the settings catalog", knob.Name)
		}
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
