package remove

import "testing"

type testEnvironment struct{ options Options }

func (e *testEnvironment) Remove(options Options) { e.options = options }

func TestCommandForwardsContractReviewChoice(t *testing.T) {
	environment := new(testEnvironment)
	command := Command(environment)
	ctx, err := command.Parse("wago plugin remove", []string{
		"--accept-contracts", "github.com/acme/plugin",
	})
	if err != nil {
		t.Fatal(err)
	}
	command.Run(ctx)
	if environment.options.Name != "github.com/acme/plugin" || !environment.options.AcceptContracts {
		t.Fatalf("options = %#v", environment.options)
	}
}
