package handoff

import (
	"slices"
	"testing"
)

func TestMetadataEnvironment(t *testing.T) {
	metadata := Metadata{
		ManagerVersion:    "canary-abc",
		ManagerExecutable: "/opt/wago",
		RuntimeChannel:    "canary",
		RuntimeProfile:    "standard",
		RuntimeBuild:      "normal",
	}
	env := metadata.Environment([]string{"PATH=/bin"})
	for _, want := range []string{
		"PATH=/bin",
		"WAGO_MANAGER_VERSION=canary-abc",
		"WAGO_MANAGER_EXECUTABLE=/opt/wago",
		"WAGO_RUNTIME_CHANNEL=canary",
		"WAGO_RUNTIME_PROFILE=standard",
		"WAGO_RUNTIME_BUILD=normal",
	} {
		if !slices.Contains(env, want) {
			t.Fatalf("environment missing %q: %#v", want, env)
		}
	}
}

func TestMetadataEnvironmentOmitsEmptyFields(t *testing.T) {
	if got := (Metadata{}).Environment([]string{"PATH=/bin"}); len(got) != 1 {
		t.Fatalf("environment = %#v", got)
	}
}

func TestFromEnvironment(t *testing.T) {
	t.Setenv(managerVersionEnv, "manager-test")
	t.Setenv(runtimeProfileEnv, "minimal")
	got := FromEnvironment()
	if got.ManagerVersion != "manager-test" || got.RuntimeProfile != "minimal" {
		t.Fatalf("metadata = %#v", got)
	}
}

func TestRuntimeOwnsPluginCommand(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"ls", "-g"}, {"inspect", "wasi"}} {
		if !RuntimeOwnsPluginCommand(args) {
			t.Fatalf("RuntimeOwnsPluginCommand(%v) = false", args)
		}
	}
	for _, args := range [][]string{nil, {"add"}, {"remove"}, {"publish"}} {
		if RuntimeOwnsPluginCommand(args) {
			t.Fatalf("RuntimeOwnsPluginCommand(%v) = true", args)
		}
	}
}

func TestLooksLikeRuntimeTarget(t *testing.T) {
	for _, value := range []string{"module.wasm", "module.wat", "build/module", "./module"} {
		if !LooksLikeRuntimeTarget(value) {
			t.Fatalf("LooksLikeRuntimeTarget(%q) = false", value)
		}
	}
	if LooksLikeRuntimeTarget("version") {
		t.Fatal("command name looked like a runtime target")
	}
}
