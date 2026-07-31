package manager

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/handoff"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestManagerOwnsPluginLifecycleAndDelegatesIntrospection(t *testing.T) {
	plugins := managerRoot.Child("plugin")
	for _, name := range []string{"add", "remove", "grant", "update", "outdated", "tree", "why", "rebuild", "verify", "publish", "unpublish", "deprecate"} {
		if plugins.Child(name) == nil {
			t.Fatalf("manager plugin group is missing %q", name)
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
	if got := strings.Join(names, ","); got != "status,update,version,auth,init,add,rm,plugin,self,cache,config" {
		t.Fatalf("manager commands = %q", got)
	}
	if managerRoot.Child("plugins") == nil {
		t.Fatal("manager command registry does not resolve plugin alias")
	}
}

func TestCoordinatedUpdateOnlyReusesMatchingSelfUpdate(t *testing.T) {
	for _, test := range []struct {
		active, manager, explicit string
		want                      bool
	}{
		{active: "canary", manager: "canary-a052caa", want: true},
		{active: "canary-a052caa", manager: "canary-f57480c", want: true},
		{active: "nightly", manager: "canary-a052caa", want: false},
		{active: "canary", manager: "canary-a052caa", explicit: "nightly", want: false},
		{active: "v0.2.0", manager: "v0.2.0", want: false},
	} {
		if got := selfUpdatesRuntime(test.active, test.manager, test.explicit); got != test.want {
			t.Errorf("selfUpdatesRuntime(%q, %q, %q) = %v, want %v", test.active, test.manager, test.explicit, got, test.want)
		}
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
