//go:build wago_profile && (linux || darwin)

package profilecmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/profcapture"
)

func TestPerfFallbackRejectsMalformedOutputPromptly(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "perf.pid")
	t.Setenv("WAGO_TEST_PERF_PID", pidFile)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\nprintf '%s' \"$$\" > \"$WAGO_TEST_PERF_PID\"\nprintf 'this is not a sample\\n'\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "perf"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m := profcapture.Manifest{Version: 1, Complete: true, Backend: "perf", Event: "cpu-clock:u", Status: wago.CodeProfileStatus{Clock: "monotonic"}}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "images.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := loadCapture(dir); done <- err }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "unexpected perf sample line") {
			t.Fatalf("lost malformed-output diagnostic: %v", err)
		}
	case <-time.After(3 * time.Second):
		// Reap only this test's producer so the old buffering behavior fails
		// promptly without leaving a sleeping process or goroutine behind.
		b, err := os.ReadFile(pidFile)
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(string(b))
		if err != nil {
			t.Fatal(err)
		}
		process, err := os.FindProcess(pid)
		if err != nil {
			t.Fatal(err)
		}
		_ = process.Kill()
		<-done
		t.Fatal("conversion waited for the producer after receiving malformed output")
	}
}
