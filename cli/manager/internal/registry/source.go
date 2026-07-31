package registry

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func UnpackedKB(dir string) int {
	total := gitTrackedSize(dir)
	if total < 0 {
		total = walkedSize(dir)
	}
	if total <= 0 {
		return 0
	}
	return int((total + 1023) / 1024)
}

func GitOutput(args ...string) string {
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return string(output)
}

func gitTrackedSize(dir string) int64 {
	output, err := exec.Command("git", "-C", dir, "ls-files", "-z").Output()
	if err != nil {
		return -1
	}
	var total int64
	for _, name := range strings.Split(string(output), "\x00") {
		if name == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			total += info.Size()
		}
	}
	return total
}

func walkedSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == ".wago" {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
