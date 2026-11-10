//go:build !windows

package build

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDecodeBuildGoJSONContextClosesInheritedStdoutOnCancel(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	release := filepath.Join(dir, "release")
	childDone := filepath.Join(dir, "child-done")
	// The shell's child retains the inherited stdout pipe after the direct
	// process is killed. Cancellation must still unblock the JSON decoder.
	script := "#!/bin/sh\n" +
		"(while [ ! -f \"$WAGO_RELEASE\" ]; do sleep 0.05; done; : > \"$WAGO_CHILD_DONE\") 2>/dev/null &\n" +
		": > \"$WAGO_READY\"\n" +
		"wait\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAGO_READY", ready)
	t.Setenv("WAGO_RELEASE", release)
	t.Setenv("WAGO_CHILD_DONE", childDone)
	defer os.WriteFile(release, nil, 0o600)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- decodeBuildGoJSONContext(ctx, dir, []string{"list"}, func(decoder *json.Decoder) error {
			var value any
			return decoder.Decode(&value)
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake Go command did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("decoder returned %v, want cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled decoder remained blocked on inherited stdout")
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(200 * time.Millisecond)
	for {
		if _, err := os.Stat(childDone); err == nil {
			t.Fatal("canceled streaming Go command left a live descendant")
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
