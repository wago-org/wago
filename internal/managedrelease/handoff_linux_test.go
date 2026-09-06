//go:build linux

package managedrelease

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestPayloadAdoptsOnePrivateLease(t *testing.T) {
	if mode := os.Getenv("WAGO_LEASE_TEST_MODE"); mode != "" {
		if mode == "child" {
			leasePath := os.Getenv("WAGO_LEASE_TEST_PATH")
			if n, err := countOpenLeaseDescriptors(leasePath); err != nil || n != 0 {
				t.Fatalf("child inherited %d leases: %v", n, err)
			}
			if os.Getenv(leaseDescriptorEnv) != "" {
				t.Fatal("handoff environment escaped into child")
			}
			return
		}
		dispatched, err := Dispatch()
		if err != nil {
			t.Fatal(err)
		}
		if dispatched {
			t.Fatal("launcher exec returned")
		}
		executable, err := ExecutablePath()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(filepath.Dir(executable), leaseFile)
		if n, err := countOpenLeaseDescriptors(path); err != nil || n != 1 {
			t.Fatalf("payload owns %d leases: %v", n, err)
		}
		if processLease.lock == nil {
			t.Fatal("payload has no private lease")
		}
		flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, processLease.lock.Descriptor(), syscall.F_GETFD, 0)
		if errno != 0 || flags&syscall.FD_CLOEXEC == 0 {
			t.Fatalf("payload lease is inheritable: flags=%d, error=%v", flags, errno)
		}
		child := exec.Command(executable, "-test.run=^TestPayloadAdoptsOnePrivateLease$")
		child.Env = append(os.Environ(), "WAGO_LEASE_TEST_MODE=child", "WAGO_LEASE_TEST_PATH="+path)
		if output, err := child.CombinedOutput(); err != nil {
			t.Fatalf("child: %v\n%s", err, output)
		}
		return
	}
	launcher := filepath.Join(t.TempDir(), executableName())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(executable, launcher); err != nil {
		t.Fatal(err)
	}
	release, err := Prepare(launcher, "handoff", func(binary, source string) error {
		if err := os.MkdirAll(source, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module test\n"), 0644); err != nil {
			return err
		}
		return CopyFile(executable, binary)
	}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := Publish(release, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{launcher, release.Binary()} {
		command := exec.Command(path, "-test.run=^TestPayloadAdoptsOnePrivateLease$")
		command.Env = append(os.Environ(), "WAGO_LEASE_TEST_MODE=payload")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("dispatch %s: %v\n%s", path, err, output)
		}
	}
}

func countOpenLeaseDescriptors(path string) (int, error) {
	var expected syscall.Stat_t
	if err := syscall.Stat(path, &expected); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil {
			return 0, fmt.Errorf("invalid descriptor entry: %w", err)
		}
		var actual syscall.Stat_t
		if syscall.Fstat(fd, &actual) == nil && actual.Dev == expected.Dev && actual.Ino == expected.Ino {
			count++
		}
	}
	return count, nil
}

func TestRejectInvalidLeaseHandoff(t *testing.T) {
	for _, value := range []string{"", "0", "2", "-1", strings.Repeat("9", 30)} {
		t.Setenv(leaseDescriptorEnv, value)
		lease, found, err := adoptProcessLease(filepath.Join(t.TempDir(), "payload"))
		if !found || err == nil || lease != nil {
			t.Fatalf("invalid handoff %q: %v, %v", value, found, err)
		}
		if _, set := os.LookupEnv(leaseDescriptorEnv); set {
			t.Fatal("invalid handoff was not consumed")
		}
	}
}
