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
		wantGlobal  bool
		wantLocal   bool
	}{
		{[]string{"--enable", "simd", "--local"}, options.Set, "simd", "on", false, false, true},
		{[]string{"--disable=inline", "--global"}, options.Set, "inline", "off", false, true, false},
		{[]string{"--set", "runtime.parallel=auto"}, options.Set, "runtime.parallel", "auto", false, false, false},
		{[]string{"--set", "simd=off", "--json"}, options.Set, "simd", "off", false, false, false},
		{[]string{"--list", "--experimental"}, options.List, "", "", true, false, false},
	} {
		environment := &testEnvironment{}
		Command(environment).Dispatch("wago config", test.args)
		got := environment.request
		if got.Action != test.wantAction || got.Key != test.wantKey || got.Value != test.wantValue || got.Experimental != test.wantPreview ||
			got.Global != test.wantGlobal || got.Local != test.wantLocal {
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

func TestConfigSubcommandsCarryScope(t *testing.T) {
	for _, test := range []struct {
		command string
		args    []string
		global  bool
		local   bool
	}{
		{"list", []string{"--global"}, true, false},
		{"get", []string{"simd", "--local"}, false, true},
		{"set", []string{"simd", "off", "-g"}, true, false},
		{"reset", []string{"simd", "-l"}, false, true},
	} {
		environment := &testEnvironment{}
		child := Command(environment).Child(test.command)
		child.Dispatch("wago config "+test.command, test.args)
		if environment.request.Global != test.global || environment.request.Local != test.local {
			t.Fatalf("%s %v scope = global %v local %v", test.command, test.args, environment.request.Global, environment.request.Local)
		}
	}
}
