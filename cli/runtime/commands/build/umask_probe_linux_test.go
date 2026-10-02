//go:build linux

package build

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
	"golang.org/x/sys/unix"
)

func TestBuildNewOutputDoesNotExposeSeparateUmaskProbe(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	watch, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Skipf("inotify unavailable: %v", err)
	}
	defer unix.Close(watch)
	if _, err := unix.InotifyAddWatch(watch, dir, unix.IN_CREATE); err != nil {
		t.Skipf("watch output directory: %v", err)
	}

	output := filepath.Join(dir, "output.wago")
	Command(testEnvironment{}).Run(command.NewContext(
		[]string{input}, map[string]string{"output": output}, nil,
	))

	buffer := make([]byte, 16<<10)
	count := 0
	for {
		n, err := unix.Read(watch, buffer)
		if err == unix.EAGAIN {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for offset := 0; offset+16 <= n; {
			nameLength := int(binary.NativeEndian.Uint32(buffer[offset+12 : offset+16]))
			next := offset + 16 + nameLength
			if next > n {
				t.Fatalf("short inotify event: offset=%d length=%d bytes=%d", offset, nameLength, n)
			}
			name := string(bytes.TrimRight(buffer[offset+16:next], "\x00"))
			if strings.HasPrefix(name, ".wago-atomic-") {
				count++
			}
			offset = next
		}
	}
	if count > 1 {
		t.Fatalf("build exposed %d atomic temporary names; want only the artifact stage", count)
	}
}
