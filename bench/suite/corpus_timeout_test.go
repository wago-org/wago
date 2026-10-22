package wagobench

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCorpusChildTimeout(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		t.Setenv("WAGO_CORPUS_TIMEOUT", "") // restore the caller's environment on cleanup
		if err := os.Unsetenv("WAGO_CORPUS_TIMEOUT"); err != nil {
			t.Fatal(err)
		}
		if got, err := corpusChildTimeout(); err != nil || got != 15*time.Second {
			t.Fatalf("default timeout = %s, %v", got, err)
		}
	})
	for _, tc := range []struct {
		value string
		want  time.Duration // zero means the setting must be rejected
	}{
		{"20s", 20 * time.Second}, // existing CI contract
		{"1ns", time.Nanosecond},
		{"1500ms", 1500 * time.Millisecond},
		{"1m", time.Minute},
		{"", 0},
		{"0", 0},
		{"0s", 0},
		{"-1s", 0},
		{"20", 0},
		{"20S", 0},
		{" 20s", 0},
		{"20s ", 0},
		{"NaN", 0},
		{"1m1ns", 0},
		{"24h", 0},
		{"9223372036854775808ns", 0}, // duration overflow
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("WAGO_CORPUS_TIMEOUT", tc.value)
			got, err := corpusChildTimeout()
			if tc.want == 0 {
				if err == nil || !strings.Contains(err.Error(), "WAGO_CORPUS_TIMEOUT") {
					t.Fatalf("invalid setting accepted or wrong error: %s, %v", got, err)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("timeout = %s, %v; want %s", got, err, tc.want)
			}
		})
	}
}

// Exercise the actual runner in a subprocess because its failures call Fatal.
// A nonexistent executable proves invalid config is rejected before Start;
// a tiny budget proves the parsed value reaches the process watchdog.
func TestCorpusTimeoutRunner(t *testing.T) {
	const probe = "WAGO_TEST_CORPUS_TIMEOUT"
	if mode := os.Getenv(probe); mode != "" {
		m := corpusIdentityFixture()
		if mode == "invalid" {
			os.Args[0] = filepath.Join(t.TempDir(), "must-not-start")
		} else if mode == "completion" {
			m = loadCorpus(t)[0]
			// A successful child with the wrong pinned identity must still fail.
			m.ArtifactSHA256 = strings.Repeat("0", 64)
		}
		runCorpusChild(t, m, "Decode", "", "explicit")
		return
	}
	for _, tc := range []struct{ mode, timeout, reason string }{
		{"invalid", "", "invalid WAGO_CORPUS_TIMEOUT"},
		{"deadline", "1ns", "corpus child timed out after 1ns"},
		{"completion", "20s", "corpus child completion: loaded artifact identity differs"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv(probe, tc.mode)
			t.Setenv("WAGO_CORPUS_TIMEOUT", tc.timeout)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCorpusTimeoutRunner$", "-test.v", "-wago.corpus=tiny")
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil || err == nil || !strings.Contains(string(out), tc.reason) {
				t.Fatalf("runner error = %v (context %v), want %q:\n%s", err, ctx.Err(), tc.reason, out)
			}
		})
	}
}
