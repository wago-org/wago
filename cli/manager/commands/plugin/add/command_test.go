package add

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
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

func TestCommandAllowAllAcceptsContractsWithoutChangingOtherFlags(t *testing.T) {
	for _, test := range []struct {
		name                             string
		flags                            []string
		grantAll, acceptContracts, force bool
	}{
		{name: "allow all", flags: []string{"--allow-all"}, grantAll: true, acceptContracts: true},
		{name: "allow all with no input", flags: []string{"--allow-all", "--no-input"}, grantAll: true, acceptContracts: true},
		{name: "explicit contracts", flags: []string{"--accept-contracts"}, acceptContracts: true},
		{name: "allow specific", flags: []string{"--allow", "host.arguments.read"}},
		{name: "deny all", flags: []string{"--deny-all"}},
		{name: "force", flags: []string{"--force"}, force: true},
		{name: "short force", flags: []string{"-f"}, force: true},
		{name: "default"},
	} {
		t.Run(test.name, func(t *testing.T) {
			automation.Reset()
			t.Cleanup(automation.Reset)
			environment := &testEnvironment{}
			cmd := Command(environment)
			ctx, err := cmd.Parse("wago add", append(test.flags, "wago-org/wasi@^0.2.0"))
			if err != nil {
				t.Fatal(err)
			}
			before := automation.Current()
			cmd.Run(ctx)
			got := environment.options
			if got.GrantAll != test.grantAll || got.AcceptContracts != test.acceptContracts || got.Force != test.force {
				t.Fatalf("options = %#v; want grantAll=%v acceptContracts=%v force=%v", got, test.grantAll, test.acceptContracts, test.force)
			}
			if automation.Current() != before {
				t.Fatal("add changed process-wide automation policy")
			}
			if !reflect.DeepEqual(got.Modules, []string{"github.com/wago-org/wasi@^0.2.0"}) {
				t.Fatalf("modules = %q", got.Modules)
			}
		})
	}
}
