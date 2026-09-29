//go:build linux

package profcapture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/profile"
	"golang.org/x/sys/unix"
)

func control(ctl, ack, command string) error {
	if ctl == "" || ack == "" {
		return fmt.Errorf("perf capture requires the record command's control handshake")
	}
	c, err := unix.Open(ctl, unix.O_RDWR|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer unix.Close(c)
	a, err := unix.Open(ack, unix.O_RDWR|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer unix.Close(a)
	if n, err := unix.Write(c, []byte(command+"\n")); err != nil || n != len(command)+1 {
		return fmt.Errorf("perf %s command: wrote %d: %v", command, n, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var reply []byte
	for time.Now().Before(deadline) {
		fds := []unix.PollFd{{Fd: int32(a), Events: unix.POLLIN}}
		_, err := unix.Poll(fds, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if fds[0].Revents&unix.POLLIN != 0 {
			var b [32]byte
			n, err := unix.Read(a, b[:])
			if err != nil {
				return err
			}
			reply = append(reply, b[:n]...)
			// perf writes sizeof(EVLIST_CTL_CMD_ACK_TAG), including the C
			// string terminator. Accept the newline-only form as well.
			if strings.Contains(string(reply), "\n") || len(reply) >= 5 {
				if string(reply) != "ack\n" && string(reply) != "ack\n\x00" {
					return fmt.Errorf("unexpected perf acknowledgement %q", reply)
				}
				return nil
			}
		}
	}
	return fmt.Errorf("perf %s acknowledgement timed out", command)
}

// RecordPerf reexecutes the capture under perf, with events disabled until the
// child receives an enable acknowledgement at its requested phase boundary.
func RecordPerf(o Options, args []string) error {
	if err := o.validateStackCapture(); err != nil {
		return err
	}
	if _, err := os.Stat(o.Out); !os.IsNotExist(err) {
		return fmt.Errorf("output must not already exist: %s", o.Out)
	}
	dir, err := os.MkdirTemp("", "wagoprof-control-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	jitDir := ""
	if o.RawStackBytes != 0 {
		// perf/libdw recognizes this literal pathname prefix when finding a JIT
		// ELF's base address. Preserve that recorded name in a portable symfs
		// tree after injection, then remove the temporary discovery directory.
		jitDir, err = os.MkdirTemp("/tmp", "jitted-wago-")
		if err != nil {
			return err
		}
		defer func() {
			if jitDir != "" {
				os.RemoveAll(jitDir)
			}
		}()
	}
	ctl, ack := filepath.Join(dir, "control"), filepath.Join(dir, "ack")
	for _, p := range []string{ctl, ack} {
		if err := unix.Mkfifo(p, 0600); err != nil {
			return err
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	raw := filepath.Join(dir, "perf.data")
	flags := []string{"record", "--clockid", "mono", "-e", "cpu-clock:u", "-F", strconv.Itoa(o.Rate), "-D", "-1", "--control=fifo:" + ctl + "," + ack, "-o", raw}
	if o.RawStackBytes != 0 {
		flags = append(flags, "--call-graph", "dwarf,"+strconv.Itoa(o.RawStackBytes))
	}
	flags = append(flags, "--", exe)
	flags = append(flags, o.CommandPrefix...)
	flags = append(flags, "capture")
	flags = append(flags, args...)
	flags = append(flags, "--control", ctl, "--ack", ack)
	if jitDir != "" {
		flags = append(flags, "--jit-dir", jitDir)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "perf", flags...)
	// A collector can stop servicing its control FIFO while its child exits
	// after an acknowledgement timeout. Preserve the failed bundle and bound
	// collector shutdown instead of waiting indefinitely for perf to notice.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout = os.Stdout
	log, err := os.Create(filepath.Join(dir, "collector.log"))
	if err != nil {
		return err
	}
	cmd.Stderr = log
	collectorDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-collectorDone:
				return
			case <-ticker.C:
				// Run writes its manifest only after teardown and export. Ignore
				// partially written JSON; a failed final manifest is authoritative.
				b, err := os.ReadFile(filepath.Join(o.Out, "manifest.json"))
				var manifest Manifest
				if err == nil && json.Unmarshal(b, &manifest) == nil && !manifest.Complete && !manifest.CollectorPending {
					cancel()
					return
				}
			}
		}
	}()
	runErr := cmd.Run()
	close(collectorDone)
	runErr = errors.Join(runErr, log.Close())
	runErr = ensureCollectorBundle(o, runErr)
	for _, name := range []string{"perf.data", "collector.log"} {
		if err := copyCaptureFile(filepath.Join(dir, name), filepath.Join(o.Out, name)); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}
	if runErr == nil {
		runErr = injectPerf(filepath.Join(o.Out, "perf.data"), filepath.Join(o.Out, "perf.jit.data"))
	}
	symbolRoot := ""
	if jitDir != "" {
		// Preserve diagnostics even when recording or injection failed. A failed
		// copy fails the capture rather than leaving a successful but unopenable
		// bundle after the private directory is removed.
		if err := copyPerfJITSymbols(jitDir, o.Out); err != nil {
			runErr = errors.Join(runErr, err)
		} else {
			symbolRoot = "symbols"
		}
		if err := os.RemoveAll(jitDir); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("remove private JIT directory: %w", err))
		} else {
			jitDir = ""
		}
	}
	if runErr == nil {
		// Raw call chains are retained for existing viewers. The temporal
		// attribution report still consumes exactly one leaf PC per sample.
		samples, err := ReadPerfSamples(filepath.Join(o.Out, "perf.data"), 10_000_000)
		if err != nil {
			runErr = fmt.Errorf("incomplete or unsupported perf capture: %w", err)
		} else {
			runErr = writeJSON(filepath.Join(o.Out, "samples.json"), samples)
			if runErr == nil {
				var events []wago.CodeProfileEvent
				b, err := os.ReadFile(filepath.Join(o.Out, "images.json"))
				if err == nil {
					err = json.Unmarshal(b, &events)
				}
				if err == nil {
					report, resolveErr := profile.Resolve(events, samples, "nanoseconds")
					err = resolveErr
					if err == nil {
						err = writeJSON(filepath.Join(o.Out, "report.json"), report)
						if err == nil {
							file, e := os.OpenFile(filepath.Join(o.Out, "native.pprof"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
							err = e
							if e == nil {
								err = errors.Join(profile.WritePprof(file, report, 0), file.Close())
							}
						}
					}
				}
				runErr = err
			}
		}
	}
	// The parent owns collector completion, which the child cannot report itself.
	path := filepath.Join(o.Out, "manifest.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return errors.Join(runErr, err)
	}
	var m Manifest
	if err = json.Unmarshal(b, &m); err != nil {
		return errors.Join(runErr, err)
	}
	m.JITSymbolRoot = symbolRoot
	return finishCollectorManifest(path, m, runErr)
}

// copyPerfJITSymbols retains the original absolute JIT names under a relative
// symbol root. `perf report --symfs BUNDLE/symbols` then works after relocation
// without restoring files in /tmp or relying on the user's build-ID cache.
func copyPerfJITSymbols(source, bundle string) error {
	if filepath.Dir(source) != "/tmp" || !strings.HasPrefix(filepath.Base(source), "jitted-wago-") {
		return fmt.Errorf("invalid private perf JIT directory %q", source)
	}
	target := filepath.Join(bundle, "symbols", strings.TrimPrefix(source, "/"))
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unexpected non-regular perf JIT file %q", entry.Name())
		}
		if err := copyCaptureFile(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
