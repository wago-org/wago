//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/internal/managedrelease"
)

func TestInstalledLauncherAliasDispatches(t *testing.T) {
	if os.Getenv("WAGO_TEST_LAUNCHER_ALIAS") == "1" {
		os.Args = append(os.Args[:1], "manager-probe")
		main()
		t.Fatal("launcher returned without dispatch")
	}
	root := t.TempDir()
	physical := filepath.Join(root, "physical")
	if err := os.MkdirAll(physical, 0755); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(root, "directory-alias")
	if err := os.Symlink(physical, aliasDir); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(aliasDir, "bin", "wago")
	if err := os.MkdirAll(filepath.Dir(launcher), 0755); err != nil {
		t.Fatal(err)
	}
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := managedrelease.CopyFile(current, launcher); err != nil {
		t.Fatal(err)
	}
	release, err := managedrelease.Prepare(launcher, "test", func(binary, source string) error {
		if err := os.MkdirAll(source, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module test\n"), 0644); err != nil {
			return err
		}
		return os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s:%s' \"$1\" \"$WAGO_SRC\"\n"), 0755)
	}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := managedrelease.Publish(release, nil, nil); err != nil {
		t.Fatal(err)
	}
	wantSource, err := filepath.EvalSymlinks(release.Source())
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "wagodev")
	if err := os.Symlink(launcher, alias); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"symlink", "argv0"} {
		t.Run(name, func(t *testing.T) {
			path := alias
			if name == "argv0" {
				path = launcher
			}
			command := exec.Command(path, "-test.run=^TestInstalledLauncherAliasDispatches$")
			if name == "argv0" {
				command.Args[0] = "private-installer"
			}
			command.Env = append(os.Environ(), "WAGO_TEST_LAUNCHER_ALIAS=1", "WAGO_SRC=", "WAGO_RELEASE_SOURCE=")
			output, err := command.CombinedOutput()
			if want := "manager-probe:" + wantSource; err != nil || string(output) != want {
				t.Fatalf("alias dispatch = %q, %v; want %q", output, err, want)
			}
		})
	}
}
