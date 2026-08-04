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
		map[string]string{"output": "app", "target": "linux/arm64"},
		map[string]bool{"local": true, "verbose": true},
	))
	want := Options{Input: "app.wasm", Output: "app", Target: "linux/arm64", Local: true, Verbose: true}
	if !reflect.DeepEqual(environment.options, want) {
		t.Fatalf("compile options = %#v, want %#v", environment.options, want)
	}
}
