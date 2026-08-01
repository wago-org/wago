package config

import (
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/manager/commands/config/completions"
	"github.com/wago-org/wago/cli/manager/commands/config/options"
)

type testEnvironment struct {
	request options.Request
}

func (environment *testEnvironment) Configure(request options.Request) { environment.request = request }
func (*testEnvironment) Completions(completions.Options)               {}

func TestConfigDefaultsToInteractiveAction(t *testing.T) {
	environment := &testEnvironment{}
	Command(environment).Dispatch("wago config", nil)
	if environment.request.Action != options.Interactive {
		t.Fatalf("action = %q", environment.request.Action)
	}
}

func TestConfigRootFlagsProvideOneShotActions(t *testing.T) {
	automation.Reset()
	t.Cleanup(automation.Reset)
	for _, test := range []struct {
		args        []string
		wantAction  options.Action
		wantKey     string
		wantValue   string
		wantPreview bool
	}{
		{[]string{"--enable", "simd"}, options.Set, "simd", "on", false},
		{[]string{"--disable=inline"}, options.Set, "inline", "off", false},
		{[]string{"--set", "runtime.parallel=auto"}, options.Set, "runtime.parallel", "auto", false},
		{[]string{"--set", "simd=off", "--json"}, options.Set, "simd", "off", false},
		{[]string{"--list", "--experimental"}, options.List, "", "", true},
	} {
		environment := &testEnvironment{}
		Command(environment).Dispatch("wago config", test.args)
		got := environment.request
		if got.Action != test.wantAction || got.Key != test.wantKey || got.Value != test.wantValue || got.Experimental != test.wantPreview {
			t.Fatalf("Dispatch(%v) = %#v", test.args, got)
		}
	}
}

func TestConfigKeepsCompletionSubcommand(t *testing.T) {
	command := Command(&testEnvironment{})
	if command.Child("completions") == nil || command.Child("ls") == nil || command.Child("set") == nil {
		t.Fatal("config command tree is incomplete")
	}
}
