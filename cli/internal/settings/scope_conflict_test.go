package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/filelock"
)

func TestLocalSettingsSaveRejectsStaleTarget(t *testing.T) {
	for _, action := range []string{"set", "reset", "reset-all", "replace", "unchanged"} {
		t.Run(action, func(t *testing.T) {
			dir := enterSettingsTestDir(t)
			t.Setenv("WAGO_CONFIG", filepath.Join(t.TempDir(), "settings.json"))
			writeTestManifest(t, dir)
			if action != "set" && action != "unchanged" {
				seed := openLocalTarget(t)
				if err := seed.Set("runtime.parallel", "2", false); err != nil {
					t.Fatal(err)
				}
				if err := seed.Save(); err != nil {
					t.Fatal(err)
				}
			}
			first, second := openLocalTarget(t), openLocalTarget(t)
			if err := first.Set("simd", "off", false); err != nil {
				t.Fatal(err)
			}
			var err error
			switch action {
			case "set":
				err = second.Set("runtime.parallel", "4", false)
			case "reset":
				err = second.Reset("runtime.parallel", false)
			case "reset-all":
				second.ResetAll()
			case "replace":
				config := second.Config()
				config.Runtime.Parallel = "4"
				err = second.Replace(config)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := first.Save(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(project.Path(dir))
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				err = second.Save()
				if err == nil || !strings.Contains(err.Error(), "settings changed") || !strings.Contains(err.Error(), "retry") {
					t.Fatalf("stale Save() = %v, want settings conflict with retry guidance", err)
				}
			}
			after, err := os.ReadFile(project.Path(dir))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("stale Save changed the manifest")
			}
			// Reopening and reapplying the edit preserves the first writer's key.
			retry := openLocalTarget(t)
			if err := retry.Set("runtime.parallel", "4", false); err != nil {
				t.Fatal(err)
			}
			if err := retry.Save(); err != nil {
				t.Fatal(err)
			}
			final := openLocalTarget(t)
			if simd, _ := final.Get("simd"); simd != "false" {
				t.Fatalf("retry lost first writer's simd override: %s", simd)
			}
			if parallel, _ := final.Get("runtime.parallel"); parallel != "4" {
				t.Fatalf("retry lost parallel override: %s", parallel)
			}
		})
	}
}

func TestGlobalSettingsSaveRejectsStaleTarget(t *testing.T) {
	enterSettingsTestDir(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv("WAGO_CONFIG", path)
	first, err := Open(true, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Set("simd", "off", false); err != nil {
		t.Fatal(err)
	}
	if err := second.Set("runtime.parallel", "4", false); err != nil {
		t.Fatal(err)
	}
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Save(); err == nil || !strings.Contains(err.Error(), "settings changed") || !strings.Contains(err.Error(), "retry") {
		t.Fatalf("stale global Save() = %v, want settings conflict with retry guidance", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("stale global Save changed the settings file")
	}
}

func TestGlobalSettingsConcurrentSavesSerializeConflictCheck(t *testing.T) {
	enterSettingsTestDir(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv("WAGO_CONFIG", path)
	first, err := Open(true, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Set("simd", "off", false); err != nil {
		t.Fatal(err)
	}
	if err := second.Set("runtime.parallel", "4", false); err != nil {
		t.Fatal(err)
	}

	gate, err := filelock.Acquire(context.Background(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	gateHeld := true
	t.Cleanup(func() {
		if gateHeld {
			_ = gate.Close()
		}
	})
	started := make(chan struct{}, 2)
	results := make(chan error, 2)
	for _, target := range []*Target{first, second} {
		go func() {
			started <- struct{}{}
			results <- target.Save()
		}()
	}
	<-started
	<-started
	var bypassErr error
	bypassed := false
	select {
	case bypassErr = <-results:
		bypassed = true
	case <-time.After(100 * time.Millisecond):
	}
	if err := gate.Close(); err != nil {
		t.Fatal(err)
	}
	gateHeld = false
	if bypassed {
		// Drain both goroutines before failing so their file operations cannot race
		// TempDir cleanup on the deliberately red, unlocked implementation.
		<-results
		t.Fatalf("Save bypassed the global transaction lock: %v", bypassErr)
	}

	var succeeded, conflicted int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case strings.Contains(err.Error(), "settings changed"):
			conflicted++
		default:
			t.Fatalf("concurrent Save() = %v, want success or stale-target conflict", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent saves succeeded/conflicted = %d/%d, want 1/1", succeeded, conflicted)
	}
}

func TestLocalSettingsSaveDetectsEachLayerChange(t *testing.T) {
	for _, key := range []string{"simd", "optimizations.inline", "runtime.parallel", "runtime.deferred-bounds-checking", "reset-all"} {
		t.Run(key, func(t *testing.T) {
			dir := enterSettingsTestDir(t)
			t.Setenv("WAGO_CONFIG", filepath.Join(t.TempDir(), "settings.json"))
			writeTestManifest(t, dir)
			first := openLocalTarget(t)
			if err := first.Set("runtime.parallel", "2", false); err != nil {
				t.Fatal(err)
			}
			if err := first.Save(); err != nil {
				t.Fatal(err)
			}
			second := openLocalTarget(t)
			if key == "reset-all" {
				first.ResetAll()
			} else {
				value := "false"
				if key == "runtime.parallel" {
					value = "4"
				}
				if err := first.Set(key, value, true); err != nil {
					t.Fatal(err)
				}
			}
			if err := first.Save(); err != nil {
				t.Fatal(err)
			}
			if err := second.Save(); err == nil {
				t.Fatal("stale Save succeeded")
			}
		})
	}
}

func TestLocalSettingsSaveRefreshesSnapshot(t *testing.T) {
	dir := enterSettingsTestDir(t)
	t.Setenv("WAGO_CONFIG", filepath.Join(t.TempDir(), "settings.json"))
	writeTestManifest(t, dir)
	target := openLocalTarget(t)
	for _, key := range []string{"simd", "optimizations.inline", "runtime.deferred-bounds-checking"} {
		if err := target.Set(key, "false", true); err != nil {
			t.Fatal(err)
		}
		if err := target.Save(); err != nil {
			t.Fatal(err)
		}
	}
	if err := target.Set("runtime.parallel", "4", false); err != nil {
		t.Fatal(err)
	}
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	if err := target.Reset("simd", false); err != nil {
		t.Fatal(err)
	}
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	target.ResetAll()
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	manifest, err := project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := manifest[localField]; ok {
		t.Fatalf("reset left settings: %#v", manifest[localField])
	}
}

func openLocalTarget(t *testing.T) *Target {
	t.Helper()
	target, err := Open(false, true)
	if err != nil {
		t.Fatal(err)
	}
	return target
}
