package clean

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct {
	options Options
}

func (environment *testEnvironment) CacheClean(options Options) {
	environment.options = options
}

func TestCleanWithoutFlagsOpensInteractiveSelection(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(nil, nil, nil))
	if environment.options.Selection.Downloads || environment.options.Selection.Builds {
		t.Fatalf("options = %#v", environment.options)
	}
}

func TestAllFlagSelectsEveryCache(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(nil, nil, map[string]bool{"all": true}))
	if !environment.options.Selection.Downloads || !environment.options.Selection.Builds {
		t.Fatalf("options = %#v", environment.options)
	}
}
