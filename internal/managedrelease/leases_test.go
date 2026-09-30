package managedrelease

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPublishedReleaseRetention(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), executableName())
	prepared := testRelease(t, launcher, "prepared")
	failed := testRelease(t, launcher, "failed")
	if err := Publish(failed, func() (func() error, error) { return nil, fmt.Errorf("bootstrap failed") }, nil); err == nil {
		t.Fatal("bootstrap succeeded")
	}
	var previous, current *Release
	for i := 0; i < 8; i++ {
		next := testRelease(t, launcher, fmt.Sprint(i))
		if err := Publish(next, nil, nil); err != nil {
			t.Fatal(err)
		}
		previous, current = current, next
	}
	for _, r := range []*Release{prepared, failed, previous, current} {
		if _, err := os.Stat(r.Binary()); err != nil {
			t.Fatalf("retained %s: %v", r.Version, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(launcher), releasesDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("retained %d directories, want current + previous + prepared + failed", len(entries))
	}
	// Re-publishing the selected release must preserve the rollback version.
	if err := Publish(current, nil, nil); err != nil {
		t.Fatal(err)
	}
	record, _, err := readRecord(filepath.Dir(launcher))
	if err != nil || record.Previous != filepath.Base(previous.Directory) {
		t.Fatalf("rollback record %+v: %v", record, err)
	}
}

func TestRunningReleaseLeaseSurvivesUpdates(t *testing.T) {
	if mode := os.Getenv("WAGO_RELEASE_LEASE_CHILD"); mode != "" {
		executable, err := ExecutablePath()
		if err != nil {
			t.Fatal(err)
		}
		// The launcher-only case models an older payload which does not pin itself.
		// It must remain protected by the dispatcher's lease across exec on Unix,
		// and while the parent dispatcher waits on Windows.
		if mode == "direct" || SourceForExecutable(executable) == "" {
			dispatched, err := Dispatch()
			if err != nil {
				t.Fatal(err)
			}
			if dispatched {
				os.Exit(0)
			}
		}
		fmt.Println("ready")
		var signal [1]byte
		if _, err := io.ReadFull(os.Stdin, signal[:]); err != nil {
			t.Fatal(err)
		}
		source, err := os.ReadFile(filepath.Join(SourceForExecutable(executable), "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Print(string(source))
		os.Exit(0)
	}
	for _, mode := range []string{"launcher-only", "direct"} {
		t.Run(mode, func(t *testing.T) {
			launcher := filepath.Join(t.TempDir(), executableName())
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			if err := CopyFile(executable, launcher); err != nil {
				t.Fatal(err)
			}
			old, err := Prepare(launcher, "live", func(binary, source string) error {
				if err := os.MkdirAll(source, 0755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("live-source"), 0644); err != nil {
					return err
				}
				return CopyFile(executable, binary)
			}, func(string) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			if err := Publish(old, nil, nil); err != nil {
				t.Fatal(err)
			}
			target := launcher
			if mode == "direct" {
				target = old.Binary()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, target, "-test.run=^TestRunningReleaseLeaseSurvivesUpdates$")
			command.Env = append(os.Environ(), "WAGO_RELEASE_LEASE_CHILD="+mode)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { input.Close(); command.Process.Kill(); command.Wait() }()
			reader := bufio.NewReader(output)
			if ready, err := reader.ReadString('\n'); err != nil || ready != "ready\n" {
				t.Fatalf("child ready %q: %v; %s", ready, err, stderr.String())
			}
			for i := 0; i < 4; i++ {
				if err := Publish(testRelease(t, launcher, fmt.Sprint(i)), nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(old.Binary()); err != nil {
				t.Fatalf("running release removed: %v", err)
			}
			if _, err := input.Write([]byte{1}); err != nil {
				t.Fatal(err)
			}
			source, readErr := io.ReadAll(reader)
			err = command.Wait()
			if err != nil || readErr != nil || string(source) != "live-source" {
				t.Fatalf("child source %q: wait %v, read %v; %s", source, err, readErr, stderr.String())
			}
			if err := Publish(testRelease(t, launcher, "last"), nil, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(old.Directory); !os.IsNotExist(err) {
				t.Fatalf("inactive release retained: %v", err)
			}
			entries, err := os.ReadDir(filepath.Join(filepath.Dir(launcher), releasesDir))
			if err != nil || len(entries) != 2 {
				t.Fatalf("retained %d directories after exit: %v", len(entries), err)
			}
		})
	}
}

func BenchmarkSelectedReleaseLease(b *testing.B) {
	launcher := filepath.Join(b.TempDir(), executableName())
	release, err := Prepare(launcher, "bench", func(binary, source string) error {
		if err := os.MkdirAll(source, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module bench"), 0644); err != nil {
			return err
		}
		return os.WriteFile(binary, nil, 0755)
	}, func(string) error { return nil })
	if err != nil {
		b.Fatal(err)
	}
	if err := Publish(release, nil, nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, lease, err := selectedLease(launcher)
		if err != nil {
			b.Fatal(err)
		}
		if err := lease.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestLaunchLeaseRacesWithPruning(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), executableName())
	if err := Publish(testRelease(t, launcher, "initial"), nil, nil); err != nil {
		t.Fatal(err)
	}
	releases := make([]*Release, 12)
	for i := range releases {
		releases[i] = testRelease(t, launcher, fmt.Sprint(i))
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, release := range releases {
			if err := Publish(release, nil, nil); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	defer func() { <-done }()
	for i := 0; i < 128; i++ {
		binary, lease, err := selectedLease(launcher)
		if err != nil {
			t.Fatal(err)
		}
		contents, binaryErr := os.ReadFile(binary)
		source, sourceErr := os.ReadFile(filepath.Join(SourceForExecutable(binary), "go.mod"))
		closeErr := lease.Close()
		if binaryErr != nil || sourceErr != nil || closeErr != nil || string(contents) != string(source) {
			t.Fatalf("leased pair %q / %q: binary %v, source %v, close %v", contents, source, binaryErr, sourceErr, closeErr)
		}
	}
}

func TestPruneResumesAfterLeaseRetirement(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), executableName())
	old := testRelease(t, launcher, "old")
	if err := Publish(old, nil, nil); err != nil {
		t.Fatal(err)
	}
	current := testRelease(t, launcher, "current")
	if err := Publish(current, nil, nil); err != nil {
		t.Fatal(err)
	}
	// A prior prune retired the lease, then could not rename the directory.
	if err := os.Rename(filepath.Join(old.Directory, leaseFile), filepath.Join(old.Directory, retiringLeaseFile)); err != nil {
		t.Fatal(err)
	}
	next := testRelease(t, launcher, "next")
	if err := Publish(next, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old.Directory); !os.IsNotExist(err) {
		t.Fatalf("retired release remains: %v", err)
	}
	if selected, err := SelectedBinary(launcher); err != nil || selected != next.Binary() {
		t.Fatalf("selection changed: %q, %v", selected, err)
	}
}
