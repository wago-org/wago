package profcapture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func workloadHash(w Workload) string {
	b, _ := json.Marshal(struct {
		Module, Init string
		Calls        []Call
	}{w.Hash, w.Init, w.Calls})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func collectorVersion() string {
	b, err := exec.Command("perf", "version").Output()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(b))
}

func commandVersion(command string, args ...string) string {
	b, err := exec.Command(command, args...).Output()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(b))
}

// BuildRevision and BuildDirty are set by scripts/build-profiler.sh. Go's build
// info may omit VCS settings for nested modules/workspaces.
var BuildRevision string
var BuildDirty string

func hostIdentity() (cpu, osVersion string) {
	switch runtime.GOOS {
	case "darwin":
		cpu = commandVersion("sysctl", "-n", "machdep.cpu.brand_string")
		osVersion = commandVersion("sw_vers", "-productVersion")
	case "linux":
		b, _ := os.ReadFile("/proc/cpuinfo")
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Hardware") {
				if _, v, ok := strings.Cut(line, ":"); ok {
					cpu = strings.TrimSpace(v)
					break
				}
			}
		}
		b, _ = os.ReadFile("/proc/sys/kernel/osrelease")
		osVersion = strings.TrimSpace(string(b))
	default:
		cpu = "unavailable"
		osVersion = "unavailable"
	}
	return
}
