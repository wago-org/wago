//go:build linux && !wago_lean

package run

import (
	"errors"
	"os"

	"github.com/wago-org/wago/cli/internal/handoff"
	"github.com/wago-org/wago/cli/internal/watchsupervisor"
)

func watchedChildLaunch() (string, []string, error) {
	manager := handoff.FromEnvironment().ManagerExecutable
	if manager == "" {
		return "", nil, errors.New("linux watch requires launch through the wago manager")
	}
	guest, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	return manager, watchsupervisor.Environment(nil, guest), nil
}
