package update

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
)

type testEnvironment struct {
	options Options
}

func TestUpdateForwardsScopeOverridesForTheResolvedGraph(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago plugin update", []string{
		"--scopes", `{"github.com/acme/plugin":{"host.import.define":{"modules":["env"]}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)
	want := map[string]map[string]project.AuthorityScope{
		"github.com/acme/plugin": {"host.import.define": {Modules: []string{"env"}}},
	}
	if !reflect.DeepEqual(environment.options.Scopes, want) {
		t.Fatalf("scopes = %#v, want %#v", environment.options.Scopes, want)
	}
}

func (environment *testEnvironment) UpdatePlugins(options Options) {
	environment.options = options
}

func TestForceFlagBypassesRevisionCheck(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago plugin update", []string{"--force", "github.com/wago-org/wasi"})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)
	if !environment.options.Force || environment.options.Module != "github.com/wago-org/wasi" {
		t.Fatalf("options = %#v", environment.options)
	}
}
