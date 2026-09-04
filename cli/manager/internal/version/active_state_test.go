package version

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestActiveStateAtomicFailureAndValidation(t *testing.T) {
	d := activeStateTestDirs(t)
	if err := setActiveInstallation(d, "old", wagopaths.ProfileStandard, wagopaths.BuildNormal); err != nil {
		t.Fatal(err)
	}
	want, err := readActiveInstallation(d)
	if err != nil {
		t.Fatal(err)
	}
	fail := errors.New("replace failed")
	next := activeInstallationState{Format: 1, Version: "new", Profile: wagopaths.ProfileMinimal, Build: wagopaths.BuildTiny}
	err = writeActiveInstallationLocked(d, next, &atomicfile.Hooks{Replace: func(string, string) error { return fail }})
	if !errors.Is(err, fail) {
		t.Fatalf("replace error: %v", err)
	}
	if err := setActiveInstallation(d, "new", "invalid", wagopaths.BuildTiny); err == nil {
		t.Fatal("accepted invalid profile")
	}
	if err := setActiveInstallation(d, "new", wagopaths.ProfileStandard, "invalid"); err == nil {
		t.Fatal("accepted invalid build")
	}
	got, err := readActiveInstallation(d)
	if err != nil || got != want {
		t.Fatalf("state changed on failure: %+v, %v", got, err)
	}
}

func TestActiveStateConcurrentSnapshots(t *testing.T) {
	d := activeStateTestDirs(t)
	if err := setActiveInstallation(d, "a", wagopaths.ProfileStandard, wagopaths.BuildNormal); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for worker := 0; worker < 2; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				ver, profile, build := "a", wagopaths.ProfileStandard, wagopaths.BuildNormal
				if worker == 1 {
					ver, profile, build = "b", wagopaths.ProfileMinimal, wagopaths.BuildTiny
				}
				if err := setActiveInstallation(d, ver, profile, build); err != nil {
					errs <- err
					return
				}
			}
		}(worker)
	}
	for i := 0; i < 40; i++ {
		state, err := readActiveInstallation(d)
		if err != nil {
			t.Fatal(err)
		}
		valid := state.Version == "a" && state.Profile == wagopaths.ProfileStandard && state.Build == wagopaths.BuildNormal || state.Version == "b" && state.Profile == wagopaths.ProfileMinimal && state.Build == wagopaths.BuildTiny
		if !valid {
			t.Fatalf("mixed state: %+v", state)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestActiveStateLegacyMigrationAndCorruption(t *testing.T) {
	d := activeStateTestDirs(t)
	if err := os.WriteFile(d.ConfigFile("active-version"), []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := readActiveInstallation(d)
	if err != nil || state.Version != "old" || state.Profile != wagopaths.ProfileStandard {
		t.Fatalf("legacy: %+v, %v", state, err)
	}
	if err := setActiveInstallation(d, "new", wagopaths.ProfileMinimal, wagopaths.BuildTiny); err != nil {
		t.Fatal(err)
	}
	if err := clearActiveInstallation(d, "new"); err != nil {
		t.Fatal(err)
	}
	state, err = readActiveInstallation(d)
	if err != nil || state.Version != "" || state.Profile != wagopaths.ProfileStandard || state.Build != wagopaths.BuildNormal {
		t.Fatalf("cleared selection = %+v, %v; want empty version with standard/normal defaults", state, err)
	}
	if err := os.WriteFile(d.ConfigFile(activeStateFile), []byte(`{"format":1,"version":"new","profile":"bad","build":"normal"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readActiveInstallation(d); err == nil {
		t.Fatal("invalid record fell back to defaults")
	}
}

func activeStateTestDirs(t *testing.T) wagopaths.Dirs {
	root := t.TempDir()
	return wagopaths.Dirs{Config: root, Data: filepath.Join(root, "data"), Versions: filepath.Join(root, "versions"), Cache: filepath.Join(root, "cache")}
}
