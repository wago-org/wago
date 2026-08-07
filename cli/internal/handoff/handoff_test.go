package handoff

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
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

func TestRuntimeCommandsCarryRoutingAndCompilationSurface(t *testing.T) {
	commands := RuntimeCommands()
	root := commandRoot(commands)
	run := root["run"]
	if run == nil || root["build"] == nil || root["validate"] == nil || root["module"] == nil {
		t.Fatalf("runtime commands = %#v", root)
	}
	flags := map[string]bool{}
	for _, flag := range run.AllFlags() {
		flags[flag.Name] = true
	}
	for _, name := range []string{"core", "parallel", "deferred-bounds-checking", "no-deferred-bounds-checking", "plugin", "bare"} {
		if !flags[name] {
			t.Errorf("runtime run description omits --%s", name)
		}
	}
	if len(run.Knobs) < 4 || !strings.HasPrefix(run.Knobs[1].Name, "no-") {
		t.Fatalf("runtime optimization description = %#v", run.Knobs)
	}
}

func commandRoot(commands []*command.Cmd) map[string]*command.Cmd {
	result := make(map[string]*command.Cmd, len(commands))
	for _, cmd := range commands {
		result[cmd.Name] = cmd
	}
	return result
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
	file := filepath.Join(t.TempDir(), "module-without-extension")
	if err := os.WriteFile(file, []byte("wasm"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"module.wasm", "MODULE.WAGO", file} {
		if !LooksLikeRuntimeTarget(value) {
			t.Fatalf("LooksLikeRuntimeTarget(%q) = false", value)
		}
	}
	for _, value := range []string{"version", "module.wat", "missing/module", "module.bin", t.TempDir()} {
		if LooksLikeRuntimeTarget(value) {
			t.Fatalf("LooksLikeRuntimeTarget(%q) = true", value)
		}
	}
}
