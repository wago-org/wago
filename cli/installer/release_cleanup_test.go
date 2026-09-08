package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReinstallCleanupPreservesReleaseIdentity(t *testing.T) {
	spellings := []string{"direct", "nested-cache"}
	if runtime.GOOS == "windows" {
		spellings = append(spellings, "case")
	} else {
		spellings = append(spellings, "external-alias", "internal-alias", "outward-alias", "outward-bin-alias")
	}
	for _, spelling := range spellings {
		t.Run(spelling, func(t *testing.T) {
			home := t.TempDir()
			root := filepath.Join(home, ".wago")
			physical := filepath.Join(root, "managed")
			if strings.HasPrefix(spelling, "outward-") {
				physical = filepath.Join(home, "external")
			}
			bin := filepath.Join(physical, "bin")
			src := filepath.Join(physical, "src")
			kept := []string{filepath.Join(bin, "wago"), filepath.Join(bin, ".wago-releases", "release-test", "src", "go.mod"), filepath.Join(src, "go.mod"), filepath.Join(bin, "cache", "keep")}
			removed := []string{filepath.Join(root, "cache", "old"), filepath.Join(root, "config", "old"), filepath.Join(root, "managed", "old")}
			for _, path := range append(append([]string{}, kept...), removed...) {
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(path), 0644); err != nil {
					t.Fatal(err)
				}
			}
			configured := physical
			if spelling == "outward-bin-alias" {
				configured = root
				for _, name := range []string{"bin", "src"} {
					if err := os.Symlink(filepath.Join(physical, name), filepath.Join(configured, name)); err != nil {
						t.Fatal(err)
					}
				}
			} else if strings.HasSuffix(spelling, "alias") {
				configured = filepath.Join(home, "alias")
				if spelling == "internal-alias" || spelling == "outward-alias" {
					configured = filepath.Join(root, "alias")
				}
				if err := os.Symlink(physical, configured); err != nil {
					t.Fatal(err)
				}
			} else if spelling == "case" {
				configured = strings.ToUpper(physical)
			}
			i := &installer{home: home, dataDir: root, configDir: filepath.Join(root, "config"), cacheDir: filepath.Join(root, "cache"), binDir: filepath.Join(configured, "bin"), srcDir: filepath.Join(configured, "src")}
			if spelling == "nested-cache" {
				i.cacheDir = filepath.Join(i.binDir, "cache")
			}
			if err := i.cleanReinstallData("full"); err != nil {
				t.Fatal(err)
			}
			for _, path := range kept {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != path {
					t.Errorf("protected data %s: %q, %v", path, data, err)
				}
			}
			if _, err := os.Stat(filepath.Join(i.binDir, "wago")); err != nil {
				t.Errorf("configured launcher path stopped resolving: %v", err)
			}
			if _, err := os.Stat(filepath.Join(i.srcDir, "go.mod")); err != nil {
				t.Errorf("configured source path stopped resolving: %v", err)
			}
			for _, path := range removed {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("obsolete data %s survived: %v", path, err)
				}
			}
		})
	}
}
