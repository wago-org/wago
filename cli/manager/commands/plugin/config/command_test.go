package config

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct{ options Options }

func (environment *testEnvironment) ConfigurePlugin(options Options) { environment.options = options }

func TestCommandForwardsInlineJSON(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext([]string{"github.com/acme/plugin", `{"mode":"safe"}`}, nil, map[string]bool{"local": true}))
	if environment.options.ID != "github.com/acme/plugin" || !environment.options.Local || !reflect.DeepEqual(environment.options.JSON, []byte(`{"mode":"safe"}`)) {
		t.Fatalf("options = %#v", environment.options)
	}
}
