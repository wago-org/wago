package version

import (
	"fmt"

	"github.com/wago-org/wago/internal/wagopaths"
)

type Toolchain struct {
	Dirs wagopaths.Dirs
}

type InstallRequest struct {
	Versions                []string
	Latest, Nightly, Canary bool
	Profile, Build          string
}

type UpdateRequest struct {
	Args            []string
	Nightly, Canary bool
	Profile, Build  string
}

func (t Toolchain) List()            { vmList(t.Dirs) }
func (t Toolchain) Current()         { vmCurrent(t.Dirs) }
func (t Toolchain) Which()           { vmWhich(t.Dirs) }
func (t Toolchain) ChooseInstalled() { vmChooseInstalled(t.Dirs) }

func (t Toolchain) Install(request InstallRequest) {
	vmInstallRequested(
		t.Dirs, request.Versions, request.Latest, request.Nightly, request.Canary,
		request.Profile, request.Build,
	)
}

func (t Toolchain) Switch(name, profileValue, buildValue string) {
	if name == "" {
		t.ChooseInstalled()
		return
	}
	profile, build := activeProfile(t.Dirs), activeBuild(t.Dirs)
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
		vmInstallForSwitch(t.Dirs, name, profile, build)
	}
	vmSwitchTo(t.Dirs, name, profile, build)
}

func (t Toolchain) Update(request UpdateRequest) {
	args := request.Args
	active := activeVersion(t.Dirs)
	if len(args) == 0 && !request.Nightly && !request.Canary {
		channel, ok := chooseUpdateChannel(active)
		if !ok {
			return
		}
		args = []string{channel}
	}
	name, err := updateVersionTarget(active, args, request.Nightly, request.Canary)
	if err != nil {
		fatal("version update: %v", err)
	}
	profile, build := activeProfile(t.Dirs), activeBuild(t.Dirs)
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
	vmUpdate(t.Dirs, name, profile, build)
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
