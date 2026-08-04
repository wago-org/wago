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
		map[string]string{"output": "app", "target": "linux/arm64", "invoke": "main", "plugin": "wasi", "plugins": "metrics,log"},
		map[string]bool{"local": true, "verbose": true},
	))
	want := Options{Input: "app.wasm", Output: "app", Target: "linux/arm64", Invoke: "main", Plugins: "wasi,metrics,log", Optimizations: map[string]bool{}, Local: true, Verbose: true}
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

func TestCommandAcceptsSingularAndPluralPluginFlags(t *testing.T) {
	for _, flag := range []string{"--plugin", "--plugins"} {
		environment := &testEnvironment{}
		Command(environment).Dispatch("wago compile", []string{flag, "wago-org/wasi", "app.wasm"})
		if environment.options.Plugins != "wago-org/wasi" {
			t.Fatalf("compile %s plugins = %q", flag, environment.options.Plugins)
		}
	}
}
