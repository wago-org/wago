package add

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/project"
)

type testEnvironment struct{ options Options }

func (environment *testEnvironment) Add(options Options) { environment.options = options }

func TestCommandForwardsNonInteractiveAuthorityChoice(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago add", []string{
		"--local", "--allow", "host.arguments.read, host.import.define", "github.com/wago-org/wasi",
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)

	if !environment.options.Local || !reflect.DeepEqual(environment.options.Authorities, []string{"host.arguments.read", "host.import.define"}) {
		t.Fatalf("options = %#v", environment.options)
	}
}

func TestCommandExpandsGitHubPluginShorthand(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago add", []string{
		"wago-org/wasi",
		"wago-org/wasi/p2",
		"wago-org/workers@^1.2.3",
		"wago-org/workers/runner@^1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)

	want := []string{
		"github.com/wago-org/wasi",
		"github.com/wago-org/wasi/p2",
		"github.com/wago-org/workers@^1.2.3",
		"github.com/wago-org/workers/runner@^1.2.3",
	}
	if !reflect.DeepEqual(environment.options.Modules, want) {
		t.Fatalf("modules = %q, want %q", environment.options.Modules, want)
	}
}

func TestCommandPreservesQualifiedPluginIDs(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext([]string{
		"github.com/wago-org/wasi",
		"gitlab.com/wago-org/wasi",
		"gopkg.in/yaml.v3",
	}, nil, nil))

	want := []string{
		"github.com/wago-org/wasi",
		"gitlab.com/wago-org/wasi",
		"gopkg.in/yaml.v3",
	}
	if !reflect.DeepEqual(environment.options.Modules, want) {
		t.Fatalf("modules = %q, want %q", environment.options.Modules, want)
	}
}

func TestCommandForwardsScopeOverridesForTheResolvedGraph(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago add", []string{
		"--scopes", `{"github.com/acme/workers":{"instance.manage":{"maxInstances":2,"maxMemoryBytes":65536}}}`,
		"github.com/acme/pool",
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)
	want := map[string]map[string]project.AuthorityScope{
		"github.com/acme/workers": {"instance.manage": {MaxInstances: 2, MaxMemoryBytes: 65536}},
	}
	if !reflect.DeepEqual(environment.options.Scopes, want) {
		t.Fatalf("scopes = %#v, want %#v", environment.options.Scopes, want)
	}
}

func TestCommandSupportsAllowAllWithoutPrompt(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(
		[]string{"github.com/wago-org/wasi"}, nil, map[string]bool{"allow-all": true},
	))
	if !environment.options.GrantAll {
		t.Fatalf("options = %#v", environment.options)
	}
}
