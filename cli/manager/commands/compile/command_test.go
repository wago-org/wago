package compile

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/settings"
)

type testEnvironment struct{ options Options }

func (environment *testEnvironment) Compile(options Options) { environment.options = options }

func TestCommandPassesStandaloneOptions(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(
		[]string{"app.wasm"},
		map[string]string{"output": "app", "target": "linux/arm64", "invoke": "main"},
		map[string]bool{"local": true, "verbose": true},
	))
	want := Options{Input: "app.wasm", Output: "app", Target: "linux/arm64", Invoke: "main", Optimizations: map[string]bool{}, Local: true, Verbose: true}
	if !reflect.DeepEqual(environment.options, want) {
		t.Fatalf("compile options = %#v, want %#v", environment.options, want)
	}
}

func TestCommandCarriesRuntimeKnobOverrides(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(
		[]string{"app.wasm"}, nil,
		map[string]bool{"deferred-bounds-checking": true, "no-inline": true},
	))
	if environment.options.DeferredBoundsChecking == nil || !*environment.options.DeferredBoundsChecking {
		t.Fatalf("deferred bounds override = %v", environment.options.DeferredBoundsChecking)
	}
	if enabled, ok := environment.options.Optimizations["inline"]; !ok || enabled {
		t.Fatalf("inline override = %v, %v", enabled, ok)
	}
}

func TestCommandExposesEveryTargetOptimizationKnob(t *testing.T) {
	command := Command(&testEnvironment{})
	have := map[string]bool{}
	for _, flag := range command.Knobs {
		have[flag.Name] = true
	}
	for _, knob := range settings.OptimizationCatalog() {
		name := strings.TrimPrefix(knob.Key, "optimizations.")
		if !have[name] || !have["no-"+name] {
			t.Errorf("compile knobs omit --%s pair", name)
		}
	}
}

func TestCommandAcceptsGlobalShortAndLongFlags(t *testing.T) {
	for _, flag := range []string{"-g", "--global"} {
		environment := &testEnvironment{}
		Command(environment).Dispatch("wago compile", []string{flag, "app.wasm"})
		if !environment.options.Global {
			t.Fatalf("compile %s did not select global plugins", flag)
		}
	}
}

func TestCommandAcceptsCoreFeatureSet(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Dispatch("wago compile", []string{"--core", "3", "app.wasm"})
	if environment.options.Core != "3" {
		t.Fatalf("compile core = %q, want 3", environment.options.Core)
	}
}

func TestCommandAcceptsRunParallelForms(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"-p", "app.wasm"}, want: "auto"},
		{args: []string{"-p8", "app.wasm"}, want: "8"},
		{args: []string{"-p", "8", "app.wasm"}, want: "8"},
		{args: []string{"--parallel", "app.wasm"}, want: "auto"},
		{args: []string{"--parallel=8", "app.wasm"}, want: "8"},
	} {
		environment := &testEnvironment{}
		Command(environment).Dispatch("wago compile", test.args)
		if environment.options.Parallel != test.want {
			t.Errorf("compile %v parallel = %q, want %q", test.args, environment.options.Parallel, test.want)
		}
	}
}
