package update

import "testing"

type testEnvironment struct{ options Options }

func (environment *testEnvironment) UpdateEverything(options Options) { environment.options = options }

func TestCommandWithoutTargetsUsesSelector(t *testing.T) {
	environment := &testEnvironment{}
	selected := Components{Manager: true, Plugins: true}
	cmd := commandWithSelector(environment, func() (Components, bool, error) {
		return selected, true, nil
	})
	context, err := cmd.Parse("wago update", nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)

	if environment.options.Components != selected || environment.options.Use != "yes" {
		t.Fatalf("options = %#v", environment.options)
	}
}

func TestCommandCanUpdateOneComponentWithoutTUI(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago update", []string{"--runtime", "--channel", "nightly", "--profile", "minimal", "--build", "tiny", "--no-use", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)

	options := environment.options
	if options.Manager || !options.Runtime || options.Plugins || !options.Force || options.Channel != "nightly" || options.Profile != "minimal" || options.Build != "tiny" || options.Use != "no" {
		t.Fatalf("options = %#v", options)
	}
}

func TestCommandSupportsPositionalTargetsWithoutTUI(t *testing.T) {
	environment := &testEnvironment{}
	selectorCalled := false
	cmd := commandWithSelector(environment, func() (Components, bool, error) {
		selectorCalled = true
		return Components{}, false, nil
	})
	context, err := cmd.Parse("wago update", []string{"manager", "plugins"})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)

	if selectorCalled {
		t.Fatal("selector opened for explicit targets")
	}
	if !environment.options.Manager || environment.options.Runtime || !environment.options.Plugins {
		t.Fatalf("options = %#v", environment.options)
	}
}

func TestCommandAllFlagSelectsEveryComponent(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago update", []string{"--all"})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)
	if environment.options.Components != allComponents() {
		t.Fatalf("components = %#v", environment.options.Components)
	}
}
