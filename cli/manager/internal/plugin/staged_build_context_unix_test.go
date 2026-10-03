//go:build linux || darwin

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/wago-org/wago/cli/internal/project"
)

func TestConfigureCancellationStopsActiveFetchAndReleasesLock(t *testing.T) {
	testConfigureCancellationAtGoPhase(t, "get")
}

func TestConfigureCancellationStopsActiveBuildAndReleasesLock(t *testing.T) {
	testConfigureCancellationAtGoPhase(t, "build")
}

func testConfigureCancellationAtGoPhase(t *testing.T, phase string) {
	t.Helper()
	buildDir, manifestDir := prepareTestPluginRuntime(t)
	const pluginID = "github.com/wago-org/wago"
	before, err := project.ReadLock(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	oldConfig := append(json.RawMessage(nil), before.Plugins[pluginID].Config...)
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	goDir := t.TempDir()
	ready := filepath.Join(goDir, "fetch-started")
	release := filepath.Join(goDir, "release-fetch")
	childExited := filepath.Join(goDir, "child-exited")
	shim := []byte("#!/bin/sh\n" +
		"if [ \"$1\" = \"$WAGO_TEST_GO_BLOCK\" ]; then\n" +
		"  (while [ ! -e \"$WAGO_TEST_GO_RELEASE\" ]; do sleep 0.02; done; : > \"$WAGO_TEST_GO_CHILD_EXIT\") &\n" +
		"  : > \"$WAGO_TEST_GO_READY\"\n" +
		"  while [ ! -e \"$WAGO_TEST_GO_RELEASE\" ]; do sleep 0.02; done\n" +
		"  exit 1\n" +
		"fi\n" +
		"exec \"$WAGO_TEST_REAL_GO\" \"$@\"\n")
	if err := os.WriteFile(filepath.Join(goDir, "go"), shim, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", goDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAGO_TEST_REAL_GO", realGo)
	t.Setenv("WAGO_TEST_GO_BLOCK", phase)
	t.Setenv("WAGO_TEST_GO_READY", ready)
	t.Setenv("WAGO_TEST_GO_RELEASE", release)
	t.Setenv("WAGO_TEST_GO_CHILD_EXIT", childExited)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Configure(ConfigRequest{Context: ctx, ID: pluginID, Config: json.RawMessage(`{"candidate":true}`), Global: true})
	}()
	deadline := time.Now().Add(20 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatalf("plugin %s finished before blocked Go command started: %v", phase, err)
		default:
		}
		if _, err := os.Stat(ready); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("plugin %s did not start", phase)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	// The shim leaves a descendant holding the Go command's output pipe.
	// Releasing it after cancellation detects whether it escaped tree shutdown.
	releaseChild := func(expectSurvivor bool) {
		t.Helper()
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		childDeadline := time.Now().Add(200 * time.Millisecond)
		for {
			if _, err := os.Stat(childExited); err == nil {
				if !expectSurvivor {
					t.Fatal("canceled Go command left a live descendant")
				}
				return
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if time.Now().After(childDeadline) {
				if expectSurvivor {
					t.Fatal("inherited-pipe test child did not exit")
				}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	select {
	case err := <-done:
		releaseChild(false)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled %s returned %v, want context cancellation", phase, err)
		}
	case <-time.After(3 * time.Second):
		// Unblock the red implementation before failing, so its subprocess and
		// project lock cannot outlive the test's temporary directories.
		releaseChild(true)
		<-done
		t.Fatalf("canceled plugin %s kept the Go command and project lock alive", phase)
	}
	probeCtx, stopProbe := context.WithTimeout(context.Background(), time.Second)
	defer stopProbe()
	if err := project.WithMutation(probeCtx, manifestDir, func(*project.Mutation) error { return nil }); err != nil {
		t.Fatalf("project lock was not released after cancellation: %v", err)
	}
	after, err := project.ReadLock(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after.Plugins[pluginID].Config, oldConfig) {
		t.Fatal("canceled plugin build published the new configuration")
	}
	staged, err := filepath.Glob(filepath.Join(filepath.Dir(buildDir), ".wago-plugin-stage-*"))
	if err != nil || len(staged) != 0 {
		t.Fatalf("canceled plugin build retained staging directories: %v, %v", staged, err)
	}
}
