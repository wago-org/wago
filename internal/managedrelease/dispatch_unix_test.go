//go:build !windows

package managedrelease

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReleaseDispatchSelectsPairedSource(t *testing.T) {
	if os.Getenv("WAGO_RELEASE_DISPATCH_TEST") == "1" {
		dispatched, err := Dispatch()
		t.Fatalf("exec returned: %v, %v", dispatched, err)
	}
	root := t.TempDir()
	physical := filepath.Join(root, "physical")
	if err := os.MkdirAll(physical, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "directory-alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(alias, executableName())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(executable, launcher); err != nil {
		t.Fatal(err)
	}
	release, err := Prepare(launcher, "test", func(binary, source string) error {
		if err := os.MkdirAll(source, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module test\n"), 0644); err != nil {
			return err
		}
		return os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' \"$WAGO_SRC\"\n"), 0755)
	}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := Publish(release, nil, nil); err != nil {
		t.Fatal(err)
	}
	wantSource, err := filepath.EvalSymlinks(release.Source())
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(launcher, "-test.run=^TestReleaseDispatchSelectsPairedSource$")
	command.Env = append(os.Environ(), "WAGO_RELEASE_DISPATCH_TEST=1", "WAGO_SRC=old-source", "WAGO_RELEASE_SOURCE=old-source")
	output, err := command.CombinedOutput()
	if err != nil || string(output) != wantSource {
		t.Fatalf("dispatch source = %q, error %v", output, err)
	}
}
