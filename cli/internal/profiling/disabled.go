//go:build !wago_profile

package profiling

import "github.com/wago-org/wago/cli/internal/command"

func Append(root *command.Cmd) *command.Cmd { return root }
func Capture([]string) bool                 { return false }

func IsInvocation([]string) bool { return false }
