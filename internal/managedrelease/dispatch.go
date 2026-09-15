package managedrelease

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Dispatch enters the selected immutable release. A payload has no selection
// record beside it and therefore continues normal startup. An explicit WAGO_SRC
// override remains authoritative; otherwise even legacy payloads get the pair.
func Dispatch() (bool, error) {
	executable, err := ExecutablePath()
	if err != nil {
		return false, err
	}
	if filepath.Base(filepath.Dir(filepath.Dir(executable))) == releasesDir {
		return false, pinProcess(executable)
	}
	target, lease, err := selectedLease(executable)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if lease != nil {
		defer lease.Close()
	}
	handoff, err := leaseHandoff(lease)
	if err != nil {
		return false, err
	}
	source := SourceForExecutable(target)
	if source == "" {
		return false, fmt.Errorf("selected manager release is incomplete")
	}
	environment := os.Environ()
	if os.Getenv("WAGO_SRC") == "" || os.Getenv("WAGO_SRC") == os.Getenv("WAGO_RELEASE_SOURCE") {
		environment = environment[:0]
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "WAGO_SRC=") && !strings.HasPrefix(entry, "WAGO_RELEASE_SOURCE=") {
				environment = append(environment, entry)
			}
		}
		environment = append(environment, "WAGO_SRC="+source, "WAGO_RELEASE_SOURCE="+source)
	}
	filtered := environment[:0]
	for _, entry := range environment {
		if !strings.HasPrefix(entry, leaseDescriptorEnv+"=") {
			filtered = append(filtered, entry)
		}
	}
	environment = filtered
	if handoff != "" {
		environment = append(environment, leaseDescriptorEnv+"="+handoff)
	}
	return true, dispatch(target, os.Args, environment)
}

// ExitCode preserves child status on platforms that cannot replace this process.
func ExitCode(err error) int {
	if e, ok := err.(*exec.ExitError); ok {
		return e.ExitCode()
	}
	if err != nil {
		return 1
	}
	return 0
}
