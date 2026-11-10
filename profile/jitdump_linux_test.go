//go:build linux && !tinygo

package profile

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

func TestJITDumpDiscoveryMapping(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJITDump(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.RefreshMarker(); err != nil {
		t.Fatal(err)
	}
	events := fixture()
	events[0].Timestamp = jitprofile.Now()
	if err = j.Write(events); err != nil {
		t.Fatal(err)
	}
	if len(j.marker) == 0 {
		t.Fatal("missing perf discovery mapping")
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("jit-%d.dump", os.Getpid())))
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(b[len(b)-16:]) != 3 {
		t.Fatal("missing terminal close")
	}
}
