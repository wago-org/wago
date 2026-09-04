package version

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/filelock"
	"github.com/wago-org/wago/internal/jsonstrict"
	"github.com/wago-org/wago/internal/regularfile"
	"github.com/wago-org/wago/internal/wagopaths"
)

const activeStateFile = "active-installation.json"

type activeInstallationState struct {
	Format  int               `json:"format"`
	Version string            `json:"version"`
	Profile wagopaths.Profile `json:"profile"`
	Build   wagopaths.Build   `json:"build"`
}

func activeStateLock(d wagopaths.Dirs) (*filelock.Lock, error) {
	return filelock.Acquire(context.Background(), d.ConfigFile("active-installation.lock"))
}

func readActiveInstallation(d wagopaths.Dirs) (activeInstallationState, error) {
	lock, err := activeStateLock(d)
	if err != nil {
		return activeInstallationState{}, err
	}
	defer lock.Close()
	return readActiveInstallationLocked(d)
}

func readActiveInstallationLocked(d wagopaths.Dirs) (activeInstallationState, error) {
	state := activeInstallationState{Format: 1, Profile: wagopaths.ProfileStandard, Build: wagopaths.BuildNormal}
	data, err := regularfile.Read(d.ConfigFile(activeStateFile), 4096)
	if os.IsNotExist(err) {
		// Legacy files are read only during migration. All new writers publish the
		// complete record and never alter the legacy tuple.
		value, err := regularfile.Read(d.ConfigFile("active-version"), 4096)
		if err != nil && !os.IsNotExist(err) {
			return state, err
		}
		state.Version = string(bytes.TrimSpace(value))
		if value, err := regularfile.Read(d.ConfigFile("active-profile"), 4096); err == nil {
			state.Profile = wagopaths.Profile(bytes.TrimSpace(value))
		} else if !os.IsNotExist(err) {
			return state, err
		}
		if value, err := regularfile.Read(d.ConfigFile("active-build"), 4096); err == nil {
			state.Build = wagopaths.Build(bytes.TrimSpace(value))
		} else if !os.IsNotExist(err) {
			return state, err
		}
	} else if err != nil {
		return state, err
	} else {
		if err := jsonstrict.ValidateTypedJSON(data, state); err != nil {
			return state, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		state = activeInstallationState{}
		if err := decoder.Decode(&state); err != nil {
			return state, err
		}
	}
	if err := validateActiveState(state); err != nil {
		return activeInstallationState{}, err
	}
	return state, nil
}

func validateActiveState(state activeInstallationState) error {
	if state.Format != 1 {
		return fmt.Errorf("unsupported active installation format %d", state.Format)
	}
	if state.Version != "" {
		if err := validateVersionStorageName(state.Version); err != nil {
			return err
		}
	}
	profile, err := wagopaths.ParseProfile(string(state.Profile))
	if err != nil || profile != state.Profile {
		return fmt.Errorf("invalid active profile %q", state.Profile)
	}
	build, err := wagopaths.ParseBuild(string(state.Build))
	if err != nil || build != state.Build {
		return fmt.Errorf("invalid active build %q", state.Build)
	}
	return nil
}

func writeActiveInstallationLocked(d wagopaths.Dirs, state activeInstallationState, hooks *atomicfile.Hooks) error {
	if err := validateActiveState(state); err != nil {
		return err
	}
	if err := atomicfile.ReplaceFile(d.ConfigFile(activeStateFile), atomicfile.Options{Mode: 0644, Sync: true, Hooks: hooks}, func(w io.Writer) error { return json.NewEncoder(w).Encode(state) }); err != nil {
		return err
	}
	return syncActiveStateDirectory(d.Config)
}

func setActiveInstallation(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) error {
	if err := validateVersionStorageName(ver); err != nil {
		return err
	}
	state := activeInstallationState{Format: 1, Version: ver, Profile: profile, Build: build}
	if err := validateActiveState(state); err != nil {
		return err
	}
	if err := d.Ensure(); err != nil {
		return err
	}
	lock, err := activeStateLock(d)
	if err != nil {
		return err
	}
	defer lock.Close()
	return writeActiveInstallationLocked(d, state, nil)
}

func clearActiveInstallation(d wagopaths.Dirs, ver string) error {
	lock, err := activeStateLock(d)
	if err != nil {
		return err
	}
	defer lock.Close()
	state, err := readActiveInstallationLocked(d)
	if err != nil || state.Version != ver {
		return err
	}
	state.Version = ""
	state.Profile, state.Build = wagopaths.ProfileStandard, wagopaths.BuildNormal
	return writeActiveInstallationLocked(d, state, nil)
}

// ActiveInstallation returns one coherent snapshot of the selected runtime.
func ActiveInstallation(d wagopaths.Dirs) (string, wagopaths.Profile, wagopaths.Build, error) {
	state, err := readActiveInstallation(d)
	return state.Version, state.Profile, state.Build, err
}

func activeTuple(d wagopaths.Dirs) (string, wagopaths.Profile, wagopaths.Build) {
	version, profile, build, err := ActiveInstallation(d)
	if err != nil {
		fatal("read active installation: %v", err)
	}
	return version, profile, build
}

func activeVersion(d wagopaths.Dirs) string {
	state, err := readActiveInstallation(d)
	if err != nil {
		return ""
	}
	return state.Version
}
func activeProfile(d wagopaths.Dirs) wagopaths.Profile {
	state, err := readActiveInstallation(d)
	if err != nil {
		return ""
	}
	return state.Profile
}
func activeBuild(d wagopaths.Dirs) wagopaths.Build {
	state, err := readActiveInstallation(d)
	if err != nil {
		return ""
	}
	return state.Build
}
