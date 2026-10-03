//go:build !linux && !darwin && !windows

package gocommand

import (
	"context"
	"os/exec"
)

// Releases target Linux, Darwin, and Windows. Keep unsupported ports
// buildable; CommandContext and WaitDelay still bound the direct Go command.
type commandTree struct{}

func prepareCommandTree(*exec.Cmd) (*commandTree, error)     { return new(commandTree), nil }
func (*commandTree) attach(context.Context, *exec.Cmd) error { return nil }
func (*commandTree) beforeWait() error                       { return nil }
func (*commandTree) terminateAndWait() error                 { return nil }
func (*commandTree) close()                                  {}
