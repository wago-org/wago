package manager

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/handoff"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestManagerCompletionUsesFullCommandTree(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	root := managerCommandRoot()
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"version", ""}, "install"},
		{[]string{"version", "install", "--p"}, "--profile"},
		{[]string{"plugin", ""}, "publish"},
		{[]string{"module", ""}, "capabilities"},
		{[]string{"run", "--i"}, "--invoke"},
	}
	for _, test := range tests {
		if got := command.Complete(root, test.args); !slices.Contains(got, test.want) {
			t.Errorf("Complete(%q) = %q, missing %q", test.args, got, test.want)
		}
	}
}

func TestManagerCommandSurfaceCoversEveryLeaf(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	root := managerCommandRoot()
	var leaves []string
	checkCommandSurface(t, root, root, nil, &leaves)
	want := strings.Join([]string{
		"status", "compile", "update",
		"version list", "version current", "version which", "version switch", "version install", "version update", "version uninstall",
		"auth login", "auth logout", "auth whoami", "init", "add", "rm",
		"plugin list", "plugin inspect", "plugin add", "plugin remove", "plugin grant", "plugin update", "plugin outdated", "plugin tree", "plugin rebuild", "plugin publish", "plugin unpublish", "plugin deprecate",
		"self update", "self uninstall", "cache dir", "cache size", "cache prune", "cache clean",
		"config list", "config diff", "config get", "config set", "config reset", "config completions",
		"run", "module imports", "module capabilities", "build", "validate",
	}, "\n")
	if got := strings.Join(leaves, "\n"); got != want {
		t.Fatalf("manager command leaves:\n%s\nwant:\n%s", got, want)
	}
}

func checkCommandSurface(t *testing.T, root, current *command.Cmd, path []string, leaves *[]string) {
	t.Helper()
	if len(path) != 0 {
		var help bytes.Buffer
		label := "wago " + strings.Join(path, " ")
		current.PrintHelp(&help, label)
		if !strings.Contains(help.String(), "Usage:") || !strings.Contains(help.String(), label) {
			t.Errorf("%s help is incomplete:\n%s", label, help.String())
		}
		candidates := command.Complete(root, append(append([]string(nil), path...), "--"))
		for _, flag := range current.AllFlags() {
			if !slices.Contains(candidates, "--"+flag.Name) {
				t.Errorf("%s completion omits --%s", label, flag.Name)
			}
		}
	}
	if len(current.Children) == 0 {
		*leaves = append(*leaves, strings.Join(path, " "))
		return
	}
	for _, child := range current.Children {
		checkCommandSurface(t, root, child, append(path, child.Name), leaves)
	}
}

func TestManagerCapturesForwardedAutomation(t *testing.T) {
	tests := []struct {
		args    []string
		json    bool
		noInput bool
		offline bool
	}{
		{args: []string{"run", "--no-input", "module.wasm"}, noInput: true},
		{args: []string{"module", "imports", "--json", "module.wasm"}, json: true},
		{args: []string{"plugin", "list", "--json", "--offline"}, json: true, offline: true},
	}
	for _, test := range tests {
		automation.Reset()
		configureInvocationAutomation(test.args)
		if automation.JSON() != test.json || automation.NoInput() != test.noInput || automation.Offline() != test.offline {
			t.Errorf("configureInvocationAutomation(%q) = %#v", test.args, automation.Current())
		}
	}
	automation.Reset()
	t.Cleanup(automation.Reset)
}

func TestManagerOwnsPluginLifecycleAndDelegatesIntrospection(t *testing.T) {
	plugins := managerRoot.Child("plugin")
	for _, name := range []string{"add", "remove", "grant", "update", "outdated", "tree", "rebuild", "publish", "unpublish", "deprecate"} {
		if plugins.Child(name) == nil {
			t.Fatalf("manager plugin group is missing %q", name)
		}
	}
	for _, name := range []string{"why", "verify"} {
		if plugins.Child(name) != nil {
			t.Fatalf("manager plugin group still exposes removed command %q", name)
		}
	}
	for _, args := range [][]string{{"list"}, {"ls"}, {"inspect", "wago-org/wasi"}} {
		if !handoff.RuntimeOwnsPluginCommand(args) {
			t.Fatalf("RuntimeOwnsPluginCommand(%v) = false", args)
		}
	}
	for _, args := range [][]string{nil, {"add"}, {"remove"}, {"grant"}, {"update"}, {"publish"}} {
		if handoff.RuntimeOwnsPluginCommand(args) {
			t.Fatalf("RuntimeOwnsPluginCommand(%v) = true", args)
		}
	}
}

func TestManagerCommandRegistry(t *testing.T) {
	var names []string
	for _, command := range managerRoot.Children {
		names = append(names, command.Name)
	}
	if got := strings.Join(names, ","); got != "status,compile,update,version,auth,init,add,rm,plugin,self,cache,config" {
		t.Fatalf("manager commands = %q", got)
	}
	if managerRoot.Child("plugins") == nil {
		t.Fatal("manager command registry does not resolve plugin alias")
	}
}

func TestManagerCommandAliases(t *testing.T) {
	tests := []struct {
		path    []string
		aliases []string
	}{
		{[]string{"status"}, []string{"st"}},
		{[]string{"update"}, []string{"up", "upgrade"}},
		{[]string{"plugin"}, []string{"plugins"}},
		{[]string{"plugin", "list"}, []string{"ls"}},
		{[]string{"plugin", "inspect"}, []string{"info", "show"}},
		{[]string{"plugin", "remove"}, []string{"rm"}},
		{[]string{"plugin", "update"}, []string{"up", "upgrade"}},
		{[]string{"plugin", "rebuild"}, []string{"build"}},
		{[]string{"version", "list"}, []string{"ls"}},
		{[]string{"version", "current"}, []string{"active"}},
		{[]string{"version", "which"}, []string{"path"}},
		{[]string{"version", "switch"}, []string{"use", "swap"}},
		{[]string{"version", "install"}, []string{"add"}},
		{[]string{"version", "update"}, []string{"up", "upgrade"}},
		{[]string{"version", "uninstall"}, []string{"remove", "rm"}},
		{[]string{"auth", "whoami"}, []string{"who"}},
		{[]string{"self", "update"}, []string{"up", "upgrade"}},
		{[]string{"cache", "clean"}, []string{"clear"}},
		{[]string{"config", "completions"}, []string{"completion"}},
	}
	for _, test := range tests {
		parent := managerRoot
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
	assertCommandTreeNamesUnique(t, managerRoot, "wago")
}

func assertCommandTreeNamesUnique(t *testing.T, parent *command.Cmd, path string) {
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
		flags := make(map[string]string)
		shorts := make(map[string]string)
		for _, flag := range child.AllFlags() {
			if previous, ok := flags[flag.Name]; ok {
				t.Errorf("%s flags --%s and --%s share a name", childPath, previous, flag.Name)
			} else {
				flags[flag.Name] = flag.Name
			}
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
		assertCommandTreeNamesUnique(t, child, childPath)
	}
}

func TestAddCommandAcceptsMultiplePackages(t *testing.T) {
	command := managerRoot.Child("add")
	if command.Args != "<module>[@version]..." {
		t.Fatalf("add args = %q", command.Args)
	}
	ctx, err := command.Parse("wago add", []string{"wago-org/wasi", "wago-org/workers@v1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Args) != 2 {
		t.Fatalf("add parsed args = %q", ctx.Args)
	}
}

func TestVersionCommandConstructor(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	versionCommand := managerRoot.Child("version")
	if len(versionCommand.Children) != 7 ||
		versionCommand.Children[0].Name != "list" ||
		versionCommand.Children[1].Name != "current" ||
		versionCommand.Children[5].Args != "[channel]" ||
		versionCommand.Children[6].Name != "uninstall" ||
		versionCommand.Children[6].Args != "[version...]" {
		t.Fatalf("version command tree = %#v", versionCommand.Children)
	}

	dirs := wagopaths.DirsFor(versionString())
	binary := dirs.VersionBinary("1.2.3")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := managerversion.SetActiveVersion(dirs, "1.2.3"); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	versionCommand.Children[0].Run(&command.Ctx{})
	versionCommand.Children[1].Run(&command.Ctx{})
	versionCommand.Children[2].Run(&command.Ctx{})
	versionCommand.Children[3].Run(&command.Ctx{Args: []string{"1.2.3"}})
	_ = w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || !strings.Contains(string(out), "1.2.3") {
		t.Fatalf("version output = %q, %v", out, err)
	}
}
