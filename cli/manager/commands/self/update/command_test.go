package update

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct{ force bool }

func (environment *testEnvironment) Update(force bool) { environment.force = force }

func TestCommandForwardsForce(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(nil, nil, map[string]bool{"force": true}))
	if !environment.force {
		t.Fatal("self update did not forward --force")
	}
}
