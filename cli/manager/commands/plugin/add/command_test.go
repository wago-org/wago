package add

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct{ options Options }

func (environment *testEnvironment) Add(options Options) { environment.options = options }

func TestCommandForwardsNonInteractiveCapabilityChoice(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago add", []string{
		"--local", "--allow", "host.environment, host.imports", "wago-org/wasi",
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)

	if !environment.options.Local || !reflect.DeepEqual(environment.options.Capabilities, []string{"host.environment", "host.imports"}) {
		t.Fatalf("options = %#v", environment.options)
	}
}

func TestCommandSupportsAllowAllWithoutPrompt(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(
		[]string{"wago-org/wasi"}, nil, map[string]bool{"allow-all": true},
	))
	if !environment.options.GrantAll {
		t.Fatalf("options = %#v", environment.options)
	}
}
