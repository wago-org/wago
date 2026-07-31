package update

import "testing"

type testEnvironment struct {
	options Options
}

func (environment *testEnvironment) UpdatePlugins(options Options) {
	environment.options = options
}

func TestForceFlagBypassesRevisionCheck(t *testing.T) {
	environment := &testEnvironment{}
	cmd := Command(environment)
	context, err := cmd.Parse("wago plugin update", []string{"--force", "wago-org/wasi"})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Run(context)
	if !environment.options.Force || environment.options.Module != "wago-org/wasi" {
		t.Fatalf("options = %#v", environment.options)
	}
}
