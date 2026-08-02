//go:build !wago_minimal

package runtime

import (
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

func TestUsageDocumentsCommandSurface(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "usage-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	usage(f)
	f.Close()
	b, _ := os.ReadFile(f.Name())
	text := string(b)
	for _, want := range []string{
		"wago is a pure-Go", // banner
		"Usage: wago",       // usage line
		"compile and execute a WebAssembly module", // run
		"precompile a WebAssembly module",          // build
		"decode and validate a module",             // validate
		"github.com/wago-org/wago",                 // footer
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("usage text missing %q:\n%s", want, text)
		}
	}
	// Only runtime-owned commands are listed. Manager commands are deliberately
	// absent from the runtime CLI.
	for _, cmd := range []string{"run", "plugin", "module", "build", "validate"} {
		if !strings.Contains(text, cmd) {
			t.Fatalf("usage text missing command %q:\n%s", cmd, text)
		}
	}
	for _, cmd := range []string{"init", "auth", "version", "self", "env", "opts"} {
		if root.Child(cmd) != nil {
			t.Fatalf("runtime command tree unexpectedly registers manager command %q", cmd)
		}
	}
	if strings.Contains(text, "test") {
		t.Fatalf("usage should no longer mention test:\n%s", text)
	}
}

func TestRuntimeHelpDoesNotInjectManagerCommands(t *testing.T) {
	oldRoot := root
	root = &command.Cmd{Name: "wago", Children: []*command.Cmd{{Name: "run", Summary: "run a module"}}}
	t.Cleanup(func() { root = oldRoot })
	t.Setenv("WAGO_MANAGER_EXECUTABLE", "/usr/local/bin/wago")

	commands := topLevelHelpCommands()
	if len(commands) != 1 || commands[0].Name != "run" {
		t.Fatalf("runtime help commands = %#v", commands)
	}
	if len(root.Children) != 1 {
		t.Fatalf("topLevelHelpCommands mutated runtime command tree: %#v", root.Children)
	}
}

func TestRuntimeCommandAndRunTargetClassification(t *testing.T) {
	if root.Child("install") != nil || root.Child("uninstall") != nil || root.Child("remove") != nil {
		t.Fatal("top-level plugin aliases collide with `wago version install/uninstall`")
	}
	plugins := root.Child("plugin")
	if plugins.Child("list") == nil || plugins.Child("inspect") == nil {
		t.Fatal("runtime plugin group does not expose list/inspect commands")
	}
	for _, name := range []string{"add", "remove", "grant", "update", "publish"} {
		if plugins.Child(name) != nil {
			t.Fatalf("runtime plugin group unexpectedly exposes manager command %q", name)
		}
	}
}

func TestRuntimeCommandAliases(t *testing.T) {
	tests := []struct {
		path    []string
		aliases []string
	}{
		{[]string{"plugin"}, []string{"plugins"}},
		{[]string{"plugin", "list"}, []string{"ls"}},
		{[]string{"plugin", "inspect"}, []string{"info", "show"}},
		{[]string{"module"}, []string{"mod"}},
		{[]string{"module", "capabilities"}, []string{"caps"}},
		{[]string{"validate"}, []string{"check"}},
	}
	for _, test := range tests {
		parent := root
		for _, name := range test.path[:len(test.path)-1] {
			parent = parent.Child(name)
			if parent == nil {
				t.Fatalf("missing command path %q", strings.Join(test.path, " "))
			}
		}
		want := parent.Child(test.path[len(test.path)-1])
		for _, alias := range test.aliases {
			if got := parent.Child(alias); got != want {
				t.Errorf("%s alias %q resolved to %#v, want %#v", strings.Join(test.path, " "), alias, got, want)
			}
		}
	}
	assertRuntimeCommandTreeNamesUnique(t, root, "wago")
}

func assertRuntimeCommandTreeNamesUnique(t *testing.T, parent *command.Cmd, path string) {
	t.Helper()
	children := make(map[string]string)
	for _, child := range parent.Children {
		childPath := path + " " + child.Name
		for _, name := range append([]string{child.Name}, child.Aliases...) {
			if previous, ok := children[name]; ok {
				t.Errorf("command name or alias %q is shared by %s and %s", name, previous, childPath)
			} else {
				children[name] = childPath
			}
		}
		shorts := make(map[string]string)
		for _, flag := range child.AllFlags() {
			if flag.Short == "" {
				continue
			}
			if len(flag.Short) != 1 {
				t.Errorf("%s flag --%s has non-single-letter short form %q", childPath, flag.Name, flag.Short)
			}
			if previous, ok := shorts[flag.Short]; ok {
				t.Errorf("%s flags --%s and --%s share -%s", childPath, previous, flag.Name, flag.Short)
			} else {
				shorts[flag.Short] = flag.Name
			}
		}
		assertRuntimeCommandTreeNamesUnique(t, child, childPath)
	}
}

func TestCommandHelpBypassesPluginRuntime(t *testing.T) {
	if !command.InvocationWantsHelp(root.Child("plugin"), nil) {
		t.Fatal("bare plugin group help was not recognized")
	}
	if !command.InvocationWantsHelp(root.Child("plugin"), []string{"--help"}) {
		t.Fatal("plugin group help was not recognized")
	}
	if !command.InvocationWantsHelp(root.Child("plugin"), []string{"list", "--global", "--help"}) {
		t.Fatal("nested plugin help was not recognized")
	}
	if !command.InvocationWantsHelp(root.Child("run"), []string{"--local", "--help"}) {
		t.Fatal("run help before the module was not recognized")
	}
	if command.InvocationWantsHelp(root.Child("run"), []string{"module.wasm", "--help"}) {
		t.Fatal("guest help after the module was intercepted")
	}
}

func TestUsageDoesNotAdvertiseValidateDirect(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "usage-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	usage(f)
	f.Close()
	b, _ := os.ReadFile(f.Name())
	removedAlias := "validate" + "-direct"
	if strings.Contains(string(b), removedAlias) {
		t.Fatalf("usage should not mention removed validate alias:\n%s", b)
	}
}
