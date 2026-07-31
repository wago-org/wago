package auth

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/manager/commands/auth/login"
)

type testEnvironment struct{}

func (testEnvironment) Login(login.Options) {}
func (testEnvironment) Logout()             {}
func (testEnvironment) Whoami()             {}

func TestCommandTree(t *testing.T) {
	cmd := Command(testEnvironment{})
	var names []string
	for _, child := range cmd.Children {
		names = append(names, child.Name)
	}
	if got := strings.Join(names, ","); got != "login,logout,whoami" {
		t.Fatalf("auth commands = %q", got)
	}
	if got := len(cmd.Children[0].Flags); got != 4 {
		t.Fatalf("login flags = %d, want 4", got)
	}
}
