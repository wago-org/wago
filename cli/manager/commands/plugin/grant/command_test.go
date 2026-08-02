package grant

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct {
	options Options
}

func (environment *testEnvironment) Grant(options Options) {
	environment.options = options
}

func TestGrantWithoutNameDefersToPackageSelector(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(nil, nil, nil))
	if environment.options.Name != "" {
		t.Fatalf("name = %q", environment.options.Name)
	}
}

func TestGrantStillAcceptsExplicitPackage(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext([]string{"wago-org/wasi"}, nil, nil))
	if environment.options.Name != "wago-org/wasi" {
		t.Fatalf("name = %q", environment.options.Name)
	}
}
