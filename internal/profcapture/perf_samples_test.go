//go:build linux || darwin

package profcapture

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func installPerfProducer(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "perf"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPerfSampleStreamPreservesWeights(t *testing.T) {
	installPerfProducer(t, `test "$1" = script || exit 1
printf '12.000000003: 2000000 abcd\n12.125000000: 3000000 0xffff\n'
`)
	samples, err := ReadPerfSamples("capture with spaces/perf.data", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[0].Timestamp != 12_000_000_003 || samples[0].Period != 2_000_000 || samples[0].PC != 0xabcd || samples[1].Timestamp != 12_125_000_000 || samples[1].PC != 0xffff {
		t.Fatalf("altered samples: %+v", samples)
	}
}

func TestPerfSampleLimitStopsProducer(t *testing.T) {
	installPerfProducer(t, `printf '1.000000000: 100 1234\n2.000000000: 100 1234\n3.000000000: 100 1234\n'
exec sleep 30
`)
	start := time.Now()
	samples, err := ReadPerfSamples("capture/perf.data", 2)
	if samples != nil || err == nil || !strings.Contains(err.Error(), "sample limit 2 exceeded") {
		t.Fatalf("partial capture accepted: %v, %v", samples, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("producer was not stopped at the sample limit")
	}
}

func TestPerfSampleStreamPropagatesProducerFailure(t *testing.T) {
	installPerfProducer(t, `printf '1.000000000: 100 1234\n'
echo 'cannot read remaining capture' >&2
exit 7
`)
	samples, err := ReadPerfSamples("capture/perf.data", 2)
	var exitErr *exec.ExitError
	if samples != nil || !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 || !strings.Contains(err.Error(), "cannot read remaining capture") {
		t.Fatalf("lost producer failure or emitted partial data: %v, %v", samples, err)
	}
}

func TestPerfConversionDiagnosticLimitStopsProducer(t *testing.T) {
	for _, inject := range []bool{false, true} {
		t.Run(map[bool]string{false: "script", true: "inject"}[inject], func(t *testing.T) {
			installPerfProducer(t, `exec awk 'BEGIN { for (i=0; i<1000000; i++) print "noisy collector diagnostic" > "/dev/stderr" }'`)
			start := time.Now()
			var err error
			if inject {
				err = injectPerf("capture/perf.data", "capture/perf.jit.data")
			} else {
				samples, e := ReadPerfSamples("capture/perf.data", 2)
				err = e
				if samples != nil {
					t.Fatal("emitted data after diagnostic loss")
				}
			}
			if err == nil || !strings.Contains(err.Error(), "diagnostic output truncated") {
				t.Fatal("diagnostic limit was not reported")
			}
			if len(err.Error()) > perfDiagnosticLimit+1024 {
				t.Fatal("unbounded diagnostic retained")
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("noisy producer was not stopped")
			}
		})
	}
}

func TestPerfSampleStreamStartFailure(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	samples, err := ReadPerfSamples("capture/perf.data", 2)
	if samples != nil || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("lost start error: %v, %v", samples, err)
	}
}
