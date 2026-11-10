//go:build linux && wago_profile

package profcapture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPerfRawStackBundlePreservesSymbols(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("raw stack capture is qualified only on AMD64")
	}
	for _, failInject := range []bool{false, true} {
		t.Run(fmt.Sprintf("inject-failure=%v", failInject), func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "capture with spaces")
			jitPathFile := filepath.Join(dir, "jit-path")
			t.Setenv("WAGO_TEST_CAPTURE", out)
			t.Setenv("WAGO_TEST_JIT_PATH", jitPathFile)
			t.Setenv("WAGO_TEST_INJECT_FAIL", fmt.Sprint(failInject))
			// This protocol test exercises parent flags, preservation, cleanup,
			// and flat report conversion. It makes no native sampling claim.
			script := `#!/bin/sh
set -eu
command="$1"
shift
case "$command" in
record)
 raw=''; jit=''; graph=''
 while [ "$#" -gt 0 ]; do
  case "$1" in
   -o) raw="$2"; shift 2 ;;
   --call-graph) graph="$2"; shift 2 ;;
   --jit-dir) jit="$2"; shift 2 ;;
   *) shift ;;
  esac
 done
 test "$graph" = dwarf,8192
 test -d "$jit"
 printf '%s' "$jit" > "$WAGO_TEST_JIT_PATH"
 printf dump > "$jit/jit-123.dump"
 printf raw > "$raw"
 mkdir "$WAGO_TEST_CAPTURE"
 printf '%s' '{"version":1,"complete":false,"collector_pending":true,"backend":"perf","raw_stack_bytes_limit":8192,"stack_collection":"perf-dwarf"}' > "$WAGO_TEST_CAPTURE/manifest.json"
 printf '%s' '[]' > "$WAGO_TEST_CAPTURE/images.json"
 # The failure monitor must not cancel a successful child awaiting collection.
 sleep 0.3
 ;;
inject)
 jit=$(cat "$WAGO_TEST_JIT_PATH")
 printf elf > "$jit/jitted-123-1.so"
 if [ "$WAGO_TEST_INJECT_FAIL" = true ]; then exit 1; fi
 while [ "$#" -gt 0 ]; do
  case "$1" in -o) printf injected > "$2"; break ;; *) shift ;; esac
 done
 ;;
script)
 hidden=false
 for arg in "$@"; do if [ "$arg" = --hide-call-graph ]; then hidden=true; fi; done
 test "$hidden" = true
 printf '12.000000003: 2000000 abcd\n'
 ;;
*) exit 1 ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "perf"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			err := RecordPerf(Options{Out: out, Backend: "perf", Rate: 99, RawStackBytes: 8192, IncludeCode: true, UnwindMaps: true}, nil)
			if (err != nil) != failInject {
				t.Fatal(err)
			}
			data, err := os.ReadFile(jitPathFile)
			if err != nil {
				t.Fatal(err)
			}
			jitDir := string(data)
			if _, err := os.Stat(jitDir); !os.IsNotExist(err) {
				t.Fatalf("temporary JIT directory retained: %v", err)
			}
			for name, want := range map[string]string{"jit-123.dump": "dump", "jitted-123-1.so": "elf"} {
				got, err := os.ReadFile(filepath.Join(out, "symbols", strings.TrimPrefix(jitDir, "/"), name))
				if err != nil || string(got) != want {
					t.Fatalf("lost diagnostic file %s: %q %v", name, got, err)
				}
			}
			data, err = os.ReadFile(filepath.Join(out, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var manifest Manifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Complete == failInject || manifest.CollectorPending || manifest.GuestStacks || manifest.RawStackBytes != 8192 || manifest.StackCollection != "perf-dwarf" || manifest.JITSymbolRoot != "symbols" {
				t.Fatal(manifest)
			}
			if !failInject {
				if _, err := os.Stat(filepath.Join(out, "native.pprof")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestPerfJITSymbolCopyRejectsNonRegularFiles(t *testing.T) {
	source, err := os.MkdirTemp("/tmp", "jitted-wago-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(source) })
	if err := os.Symlink("/does-not-exist", filepath.Join(source, "bad.so")); err != nil {
		t.Fatal(err)
	}
	if err := copyPerfJITSymbols(source, t.TempDir()); err == nil {
		t.Fatal("symlink accepted as a captured JIT image")
	}
}

func TestPerfControlAcknowledgement(t *testing.T) {
	for _, reply := range []string{"ack\n", "ack\n\x00", "bad\n", "ack\nextra", "xxxxx"} {
		t.Run(fmt.Sprintf("%q", reply), func(t *testing.T) {
			dir := t.TempDir()
			ctl, ack := filepath.Join(dir, "ctl"), filepath.Join(dir, "ack")
			for _, p := range []string{ctl, ack} {
				if err := unix.Mkfifo(p, 0600); err != nil {
					t.Fatal(err)
				}
			}
			c, err := os.OpenFile(ctl, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			a, err := os.OpenFile(ack, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			for _, command := range []string{"enable", "disable"} {
				done := make(chan string, 1)
				go func() {
					var b [32]byte
					n, err := c.Read(b[:])
					if err != nil {
						done <- err.Error()
						return
					}
					_, err = a.Write([]byte(reply))
					if err != nil {
						done <- err.Error()
						return
					}
					done <- string(b[:n])
				}()
				err := control(ctl, ack, command)
				valid := reply == "ack\n" || reply == "ack\n\x00"
				if (err == nil) != valid {
					t.Fatalf("reply %q: %v", reply, err)
				}
				if got := <-done; got != command+"\n" {
					t.Fatal(got)
				}
			}
		})
	}
}

func TestDeniedPerfPreservesFailedManifest(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "perf")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho denied >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out := filepath.Join(dir, "capture")
	err := RecordPerf(Options{Out: out, Backend: "perf", Rate: 499}, nil)
	if err == nil {
		t.Fatal("collector error ignored")
	}
	b, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err = json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Complete || len(m.Diagnostics) == 0 {
		t.Fatal(m)
	}
	b, err = os.ReadFile(filepath.Join(out, "collector.log"))
	if err != nil || !strings.Contains(string(b), "denied") {
		t.Fatal(string(b), err)
	}
}

func TestInterruptedChildManifestRetainsRawPerfCapture(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "capture")
	t.Setenv("WAGO_PERF_TEST_OUT", out)
	script := `#!/bin/sh
if [ "$1" != record ]; then
 echo "conversion must not run" >&2
 exit 19
fi
while [ "$#" -gt 0 ]; do
 if [ "$1" = -o ]; then shift; raw=$1; break; fi
 shift
done
mkdir "$WAGO_PERF_TEST_OUT"
printf '%s' '{"version":1,"complete":true,' > "$WAGO_PERF_TEST_OUT/manifest.json"
printf '%s' 'partial raw capture' > "$raw"
echo 'child interrupted while exporting' >&2
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, "perf"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	err := RecordPerf(Options{Out: out, Backend: "perf", Rate: 99}, nil)
	if err == nil || !strings.Contains(err.Error(), "read final capture manifest") {
		t.Fatalf("missing original failure: %v", err)
	}
	var m Manifest
	b, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 || m.Complete || m.Iterations != 0 || m.Backend != "perf" {
		t.Fatalf("invalid failed bundle: %+v", m)
	}
	for name, want := range map[string]string{
		"manifest.child.invalid.json": `{"version":1,"complete":true,`,
		"perf.data":                   "partial raw capture", "collector.log": "child interrupted while exporting\n",
	} {
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil || string(b) != want {
			t.Fatalf("lost %s: %q, %v", name, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "perf.jit.data")); !os.IsNotExist(err) {
		t.Fatalf("converted incomplete capture: %v", err)
	}
}

func TestFailedChildStopsPerfCollector(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "capture")
	t.Setenv("WAGO_PERF_TEST_OUT", out)
	fake := filepath.Join(dir, "perf")
	// Model a collector that ignores its failed child and even ignores interrupt.
	// Exec replaces the shell so forced shutdown cannot leave a waiting shell.
	script := `#!/bin/sh
mkdir "$WAGO_PERF_TEST_OUT"
printf '%s' '{"version":1,"complete":false,"diagnostics":["child control timeout"]}' > "$WAGO_PERF_TEST_OUT/manifest.json"
trap '' INT
exec sleep 30
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	start := time.Now()
	if err := RecordPerf(Options{Out: out, Backend: "perf", Rate: 99}, nil); err == nil || !strings.Contains(err.Error(), "child control timeout") {
		t.Fatalf("original child failure was not returned: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("collector shutdown took %v", elapsed)
	}
	raw, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Complete || len(m.Diagnostics) < 2 || m.Diagnostics[0] != "child control timeout" {
		t.Fatalf("lost child failure: %+v", m)
	}
}

func TestSilentConversionFailsManifestAndPreservesRaw(t *testing.T) {
	for _, stage := range []string{"inject", "script"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "capture")
			t.Setenv("WAGO_TEST_CAPTURE", out)
			t.Setenv("WAGO_HANG_STAGE", stage)
			installPerfProducer(t, `case "$1" in
record)
 while [ "$#" -gt 0 ]; do if [ "$1" = -o ]; then printf raw > "$2"; break; fi; shift; done
 mkdir "$WAGO_TEST_CAPTURE"
 printf '%s' '{"version":1,"complete":false,"collector_pending":true,"backend":"perf"}' > "$WAGO_TEST_CAPTURE/manifest.json"
 printf '[]' > "$WAGO_TEST_CAPTURE/images.json"
 ;;
inject|script)
 if [ "$1" = "$WAGO_HANG_STAGE" ]; then exec sleep 30; fi
 ;;
esac`)
			started := time.Now()
			err := RecordPerf(Options{Out: out, Backend: "perf", ConversionTimeout: 150 * time.Millisecond}, nil)
			if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
				t.Fatalf("missing deadline: %v", err)
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("conversion hung")
			}
			raw, e := os.ReadFile(filepath.Join(out, "perf.data"))
			if e != nil || string(raw) != "raw" {
				t.Fatal("raw evidence lost", e)
			}
			var m Manifest
			b, e := os.ReadFile(filepath.Join(out, "manifest.json"))
			if e != nil {
				t.Fatal(e)
			}
			if e = json.Unmarshal(b, &m); e != nil {
				t.Fatal(e)
			}
			if m.Complete || m.CollectorPending || !strings.Contains(strings.Join(m.Diagnostics, " "), "deadline exceeded") {
				t.Fatalf("invalid failure manifest: %+v", m)
			}
		})
	}
}
