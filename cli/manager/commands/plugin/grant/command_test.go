package grant

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/project"
)

type testEnvironment struct {
	options Options
}

func TestGrantHelpDocumentsSingleFullGraphScopeDocument(t *testing.T) {
	var output bytes.Buffer
	Command(&testEnvironment{}).PrintHelp(&output, "wago plugin grant")
	help := output.String()
	for _, want := range []string{"--scopes <json>", "full Plugin ID", "host.import.define", "modules"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q:\n%s", want, help)
		}
	}
}

func TestGrantForwardsExactScopeOverrides(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago plugin grant", []string{
		"--scopes", `{"github.com/acme/plugin":{"host.import.define":{"modules":["env"]}}}`,
		"github.com/acme/plugin",
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

func TestGrantAllowsPickingAPluginWhenNoIDIsSupplied(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago plugin grant", nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)
	if environment.options.Name != "" {
		t.Fatalf("name = %q, want picker selection", environment.options.Name)
	}
}

func (environment *testEnvironment) Grant(options Options) {
	environment.options = options
}

func TestGrantRequiresExplicitFullPluginID(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext([]string{"github.com/wago-org/wasi"}, nil, nil))
	if environment.options.Name != "github.com/wago-org/wasi" {
		t.Fatalf("name = %q", environment.options.Name)
	}
}
