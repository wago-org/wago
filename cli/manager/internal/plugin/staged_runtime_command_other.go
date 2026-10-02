//go:build !linux && !darwin && !windows

package plugin

import (
	"context"
	"errors"
	"os/exec"
)

func stagedRuntimeCommand(context.Context, string) (*exec.Cmd, error) {
	return nil, errors.New("staged runtime process containment is unsupported on this platform")
}
