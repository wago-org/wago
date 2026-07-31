package login

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

type testEnvironment struct {
	options Options
}

func (e *testEnvironment) Login(options Options) { e.options = options }

func TestRunOwnsLoginFlagParsing(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Run(command.NewContext(
		nil,
		map[string]string{"token": "secret"},
		map[string]bool{"with-token": true},
	))
	if environment.options.Token != "secret" || !environment.options.WithToken {
		t.Fatalf("options = %#v", environment.options)
	}
}
