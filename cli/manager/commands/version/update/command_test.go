package update

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct {
	args            []string
	nightly, canary bool
	force           bool
	profile, build  string
	use             string
}

func (e *testEnvironment) UpdateVersion(args []string, nightly, canary, force bool, profile, build, use string) {
	e.args = append([]string(nil), args...)
	e.nightly, e.canary = nightly, canary
	e.force = force
	e.profile, e.build = profile, build
	e.use = use
}

func TestRunDelegatesInteractiveSelectionWhenTargetIsOmitted(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(nil, nil, nil))
	if len(environment.args) != 0 {
		t.Fatalf("args = %#v", environment.args)
	}
}

func TestRunForwardsOptions(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(
		[]string{"nightly"},
		map[string]string{"profile": "minimal", "build": "tiny"},
		map[string]bool{"nightly": true, "canary": true, "force": true},
	))
	if len(environment.args) != 1 || environment.args[0] != "nightly" ||
		!environment.nightly || !environment.canary || !environment.force ||
		environment.profile != "minimal" || environment.build != "tiny" {
		t.Fatalf("update = %#v", environment)
	}
}
