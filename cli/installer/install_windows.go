//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func platformDirs(home, wagoRoot string, explicitHome bool) (data, config, cache string) {
	if explicitHome {
		return filepath.Join(wagoRoot, "data"), filepath.Join(wagoRoot, "config"), filepath.Join(wagoRoot, "cache")
	}
	return wagoRoot, filepath.Join(wagoRoot, "config"), filepath.Join(wagoRoot, "cache")
}

func pathTargets(home string) []pathTarget {
	return []pathTarget{{label: "Command Prompt", description: "User PATH", shell: "cmd", current: true}}
}

func pathSetupQuestion() string { return "Add Wago to your user PATH?" }

func pathSetupTargetMessage(target pathTarget, home string) string { return "" }

func addPath(binDir, configFile, shellName string) (bool, error) {
	userPath, pathType := os.Getenv("WAGO_TEST_USER_PATH"), "REG_EXPAND_SZ"
	if userPath == "" {
		output, err := exec.Command("reg.exe", "query", `HKCU\Environment`, "/v", "Path").CombinedOutput()
		if err == nil {
			fields := strings.Fields(string(output))
			for index, field := range fields {
				if strings.EqualFold(field, "Path") && index+2 < len(fields) {
					pathType, userPath = fields[index+1], strings.Join(fields[index+2:], " ")
					break
				}
			}
		}
	}
	for _, path := range filepath.SplitList(userPath) {
		if strings.EqualFold(filepath.Clean(path), filepath.Clean(binDir)) {
			return true, nil
		}
	}
	newPath := binDir
	if userPath != "" {
		newPath += ";" + userPath
	}
	if os.Getenv("WAGO_TEST_USER_PATH") == "" {
		if output, err := exec.Command("reg.exe", "add", `HKCU\Environment`, "/v", "Path", "/t", pathType, "/d", newPath, "/f").CombinedOutput(); err != nil {
			return false, fmt.Errorf("update user PATH: %w: %s", err, output)
		}
	}
	_ = os.Setenv("PATH", binDir+";"+os.Getenv("PATH"))
	return false, nil
}

func cleanPlatformInstall(mode, home, binDir, srcDir, dataDir, configDir, cacheDir string) error {
	if mode == "minimal" {
		return nil
	}
	paths := []string{filepath.Join(binDir, "wago.exe"), filepath.Join(dataDir, "versions"), configDir, cacheDir, srcDir}
	if mode == "full" {
		paths = append(paths, dataDir, filepath.Join(home, ".wago"))
		if filepath.Base(dataDir) == "data" && filepath.Dir(configDir) == filepath.Dir(dataDir) && filepath.Dir(cacheDir) == filepath.Dir(dataDir) {
			paths = append(paths, filepath.Dir(dataDir))
		}
	}
	for _, path := range uniquePaths(paths) {
		if err := safeRemove(path, home); err != nil {
			return err
		}
	}
	return nil
}

func safeRemove(path, home string) error {
	path = filepath.Clean(path)
	volume := filepath.VolumeName(path)
	if path == "." || strings.EqualFold(path, filepath.Clean(home)) || strings.EqualFold(path, volume+`\`) {
		return fmt.Errorf("refusing to remove unsafe path %q", path)
	}
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		key := strings.ToLower(filepath.Clean(path))
		if key != "." && !seen[key] {
			seen[key] = true
			result = append(result, filepath.Clean(path))
		}
	}
	return result
}

func pathContains(binDir string) bool {
	for _, path := range filepath.SplitList(os.Getenv("PATH")) {
		if strings.EqualFold(filepath.Clean(path), filepath.Clean(binDir)) {
			return true
		}
	}
	return false
}

func shellFromConfig(configFile string) string { return "" }
func sourceCommand(configFile string) string   { return "" }
