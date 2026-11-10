//go:build !wago_profile

package profiling

import (
	"github.com/wago-org/wago/cli/internal/command"
	"testing"
)

func TestProfileCommandIsAbsent(t *testing.T) {
	root := &command.Cmd{Name: "wago"}
	if Append(root) != root || root.Child("profile") != nil || Capture([]string{"profile", "capture"}) {
		t.Fatal("ordinary build exposes profiling")
	}
}
