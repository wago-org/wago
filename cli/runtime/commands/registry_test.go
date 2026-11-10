package commands

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
)

func TestRegistryPreservesCommandOrderAndAliases(t *testing.T) {
	root := Registry(
		&command.Cmd{Name: "run"},
		&command.Cmd{Name: "plugin", Aliases: []string{"plugins"}},
		&command.Cmd{Name: "module"},
	)
	var names []string
	for _, child := range root.Children {
		names = append(names, child.Name)
	}
	if got := strings.Join(names, ","); got != "run,plugin,module" {
		t.Fatalf("runtime commands = %q", got)
	}
	if root.Child("plugins") == nil {
		t.Fatal("runtime registry does not resolve command aliases")
	}
}
