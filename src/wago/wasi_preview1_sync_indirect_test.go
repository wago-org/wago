//go:build linux && amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"os"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// TestSyncHostLinkedCallIndirectUsesWrapperDescriptors is reduced from the
// preview-1 blake3sum corpus crash. The declared returning import forces the
// synchronous-host link path, while _start reaches only local call_indirect
// targets. Register-ABI-tagged descriptors corrupted that path before any host
// callback ran and jumped to a low non-code address.
func TestSyncHostLinkedCallIndirectUsesWrapperDescriptors(t *testing.T) {
	src, err := os.ReadFile("testdata/wasi-preview1-sync-indirect.wasm")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(nil, src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !compiled.needsLink {
		t.Fatal("reduced returning-import module did not defer linking")
	}

	hostCalled := false
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"wasi_snapshot_preview1.environ_sizes_get": HostFunc(func(_ HostModule, _ []uint64, results []uint64) {
			hostCalled = true
			results[0] = 0
		}),
	}})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if !in.c.syncHostImports {
		t.Fatal("linked module did not select synchronous host imports")
	}
	for fidx := in.c.NumImports; fidx < len(in.c.FuncTypeID); fidx++ {
		off := (fidx + 1) * coreruntime.TableEntryBytes
		home := binary.LittleEndian.Uint64(in.funcRefDescs[off+coreruntime.TableEntryHomeLinMemOffset:])
		if home>>63 != 0 {
			t.Fatalf("local function %d retained a register-ABI descriptor in synchronous-host mode", fidx)
		}
	}
	if _, err := in.Invoke("_start"); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if hostCalled {
		t.Fatal("reduced local-indirect fixture unexpectedly called its declared host import")
	}
}
