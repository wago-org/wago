package version

import (
	"context"
	"fmt"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/internal/wagopaths"
)

type Toolchain struct {
	Dirs    wagopaths.Dirs
	Context context.Context
}

func (t Toolchain) context() context.Context {
	if t.Context != nil {
		return t.Context
	}
	return context.Background()
}

type InstallRequest struct {
	Versions                []string
	Latest, Nightly, Canary bool
	Profile, Build          string
	Use                     string
}

type UpdateRequest struct {
	Args            []string
	Nightly, Canary bool
	Force           bool
	Profile, Build  string
	Use             string
}

func (t Toolchain) List()            { vmList(t.Dirs) }
func (t Toolchain) Current()         { vmCurrent(t.Dirs) }
func (t Toolchain) Which()           { vmWhich(t.Dirs) }
func (t Toolchain) ChooseInstalled() { vmChooseInstalled(t.Dirs) }

func (t Toolchain) Install(request InstallRequest) {
	vmInstallRequestedContext(
		t.context(), t.Dirs, request.Versions, request.Latest, request.Nightly, request.Canary,
		request.Profile, request.Build,
		request.Use,
	)
}

func (t Toolchain) Switch(name, profileValue, buildValue string) {
	if name == "" {
		t.ChooseInstalled()
		return
	}
	_, profile, build := activeTuple(t.Dirs)
	var err error
	if profileValue != "" {
		profile, err = wagopaths.ParseProfile(profileValue)
		if err != nil {
			fatal("version switch: %v", err)
		}
	}
	if buildValue != "" {
		build, err = wagopaths.ParseBuild(buildValue)
		if err != nil {
			fatal("version switch: %v", err)
		}
	}
	if _, _, _, installed := installedRuntime(t.Dirs, name, profile, build); !installed {
		vmInstallForSwitchContext(t.context(), t.Dirs, name, profile, build)
	}
	vmSwitchTo(t.Dirs, name, profile, build)
}

func (t Toolchain) Update(request UpdateRequest) {
	args := request.Args
	active, profile, build := activeTuple(t.Dirs)
	if len(args) == 0 && !request.Nightly && !request.Canary {
		if automation.NoInput() {
			if !isRollingChannel(active) {
				fatal("version update: --no-input requires [channel], --nightly, or --canary when the active runtime is pinned")
			}
			args = []string{active}
		} else {
			channel, ok := chooseUpdateChannel(active)
			if !ok {
				return
			}
			args = []string{channel}
		}
	}
	name, err := updateVersionTarget(active, args, request.Nightly, request.Canary)
	if err != nil {
		fatal("version update: %v", err)
	}
	if request.Profile != "" {
		profile, err = wagopaths.ParseProfile(request.Profile)
		if err != nil {
			fatal("version update: %v", err)
		}
	}
	if request.Build != "" {
		build, err = wagopaths.ParseBuild(request.Build)
		if err != nil {
			fatal("version update: %v", err)
		}
	}
	if name == "" {
		fatal("version update: %v", fmt.Errorf("no release channel selected"))
	}
	vmUpdateContext(t.context(), t.Dirs, name, profile, build, request.Use, request.Force)
}

func (t Toolchain) UninstallAll() {
	for _, name := range installedVersions(t.Dirs) {
		vmUninstall(t.Dirs, name)
	}
}

func (t Toolchain) Uninstall(names []string) {
	if len(names) == 0 {
		vmChooseUninstall(t.Dirs)
		return
	}
	for _, name := range names {
		vmUninstall(t.Dirs, name)
	}
}
