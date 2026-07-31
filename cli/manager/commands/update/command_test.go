package update

import "testing"

type testEnvironment struct{ options Options }

func (environment *testEnvironment) UpdateEverything(options Options) { environment.options = options }

func TestCommandDefaultsToUpdatingEverything(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago update", nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)

	if !environment.options.Self || !environment.options.Runtime || !environment.options.Plugins || environment.options.Use != "yes" {
		t.Fatalf("options = %#v", environment.options)
	}
}

func TestCommandCanUpdateOneComponentWithoutTUI(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago update", []string{"--runtime", "--channel", "nightly", "--profile", "minimal", "--build", "tiny", "--no-use"})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)

	options := environment.options
	if options.Self || !options.Runtime || options.Plugins || options.Channel != "nightly" || options.Profile != "minimal" || options.Build != "tiny" || options.Use != "no" {
		t.Fatalf("options = %#v", options)
	}
}
