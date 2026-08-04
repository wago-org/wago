package compile

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
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
	want := Options{Input: "app.wasm", Output: "app", Target: "linux/arm64", Invoke: "main", Plugins: "wasi,metrics,log", Local: true, Verbose: true}
	if !reflect.DeepEqual(environment.options, want) {
		t.Fatalf("compile options = %#v, want %#v", environment.options, want)
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
