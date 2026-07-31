package version

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/manager/commands/version/install"
)

type testEnvironment struct{}

func (testEnvironment) List()                                                      {}
func (testEnvironment) Current()                                                   {}
func (testEnvironment) Which()                                                     {}
func (testEnvironment) Switch(string, string, string)                              {}
func (testEnvironment) InstallRequested(install.Options)                           {}
func (testEnvironment) UpdateVersion([]string, bool, bool, string, string, string) {}
func (testEnvironment) UninstallVersions([]string)                                 {}
func (testEnvironment) UninstallAllVersions()                                      {}

func TestCommandTree(t *testing.T) {
	cmd := Command(testEnvironment{})
	var names []string
	for _, child := range cmd.Children {
		names = append(names, child.Name)
	}
	if got := strings.Join(names, ","); got != "list,current,which,switch,install,update,uninstall" {
		t.Fatalf("version commands = %q", got)
	}
	if cmd.Children[3].Aliases[0] != "swap" || cmd.Children[4].Aliases[0] != "add" {
		t.Fatalf("version aliases = switch:%v install:%v", cmd.Children[3].Aliases, cmd.Children[4].Aliases)
	}
}
