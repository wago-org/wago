//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func ownedMultiMemorySnapshotModule() []byte {
	addr0 := wasmtest.ULEB(65536)
	addr1 := wasmtest.ULEB(2 * 65536)
	warm := []byte{0x41, 0x01, 0x40, 0x00, 0x1a, 0x41, 0x01, 0x40, 0x01, 0x1a}
	warm = append(warm, 0x41)
	warm = append(warm, addr0...)
	warm = append(warm, 0x41)
	warm = append(warm, wasmtest.SLEB32(0x55)...)
	warm = append(warm, 0x36, 0x02, 0x00)
	warm = append(warm, 0x41)
	warm = append(warm, addr1...)
	warm = append(warm, 0x41)
	warm = append(warm, wasmtest.SLEB32(0x66)...)
	warm = append(warm, 0x36, 0x42, 0x01, 0x00)
	warm = append(warm,
		0x41, 0x20, 0x41, 0x00, 0x41, 0x04, // dst=32, src=0, len=4
		0xfc, 0x08, 0x00, 0x01, // memory.init data 0 memory 1
		0xfc, 0x09, 0x00, // data.drop 0
		0x0b,
	)
	load0 := append([]byte{0x41}, addr0...)
	load0 = append(load0, 0x28, 0x02, 0x00, 0x0b)
	load1 := append([]byte{0x41}, addr1...)
	load1 = append(load1, 0x28, 0x42, 0x01, 0x00, 0x0b)
	initAfterDrop := []byte{
		0x41, 0x30, 0x41, 0x00, 0x41, 0x01,
		0xfc, 0x08, 0x00, 0x01,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1),
			wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(0),
		)),
		wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x01, 0x03},
			[]byte{0x01, 0x02, 0x04},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("warm", 0, 0),
			wasmtest.ExportEntry("size0", 0, 1),
			wasmtest.ExportEntry("size1", 0, 2),
			wasmtest.ExportEntry("load0", 0, 3),
			wasmtest.ExportEntry("load1", 0, 4),
			wasmtest.ExportEntry("init1", 0, 5),
			wasmtest.ExportEntry("m0", 2, 0),
			wasmtest.ExportEntry("m1", 2, 1),
		)),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(warm),
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0x01, 0x0b}),
			wasmtest.Code(load0),
			wasmtest.Code(load1),
			wasmtest.Code(initAfterDrop),
		)),
		wasmtest.Section(11, wasmtest.Vec(
			append([]byte{0x01}, append(wasmtest.ULEB(4), []byte("snap")...)...),
		)),
	)
}

func loadStagedMultiMemorySnapshot(blob []byte) (*Snapshot, error) {
	return loadSnapshotWith(blob, func(compiled []byte) (*Compiled, error) {
		if !IsCompiled(compiled) {
			return nil, errors.New("compiled snapshot payload is not a wago module")
		}
		c := &Compiled{}
		if err := unmarshalCompiled(c, compiled[5:]); err != nil {
			return nil, err
		}
		if c.memoryDir == nil {
			c.memoryDir = &compiledMemoryDirectory{}
		}
		c.memoryDir.staged = true
		c.memoryDir.exactExports = true
		return c, nil
	})
}

func assertOwnedMultiMemorySnapshotState(t *testing.T, snapshot *Snapshot) {
	t.Helper()
	in, err := Instantiate(snapshot)
	if err != nil {
		t.Fatalf("instantiate snapshot: %v", err)
	}
	defer in.Close()
	if got := string(in.memoryDir.memories[1].Bytes()[32:36]); got != "snap" {
		t.Fatalf("restored internal memory 1 bytes = %q, want snap", got)
	}
	if got := tableTestCallI32(t, in, "size0"); got != 2 {
		t.Fatalf("restored memory 0 pages = %d, want 2", got)
	}
	if got := tableTestCallI32(t, in, "size1"); got != 3 {
		t.Fatalf("restored memory 1 pages = %d, want 3", got)
	}
	if got := tableTestCallI32(t, in, "load0"); got != 0x55 {
		t.Fatalf("restored memory 0 grown-page value = %#x, want 0x55", got)
	}
	if got := tableTestCallI32(t, in, "load1"); got != 0x66 {
		t.Fatalf("restored memory 1 grown-page value = %#x, want 0x66", got)
	}
	m1, err := in.ExportedMemory("m1")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(m1.Bytes()[32:36]); got != "snap" {
		t.Fatalf("restored memory.init bytes = %q, want snap", got)
	}
	if _, err := in.Invoke("init1"); err == nil {
		t.Fatal("restored dropped passive data segment unexpectedly initialized memory")
	}
	if len(in.memoryDir.native) != 32 {
		t.Fatalf("native memory directory bytes = %d, want 32", len(in.memoryDir.native))
	}
	for i, wantPages := range []uint32{2, 3} {
		entry := in.memoryDir.native[i*16:]
		if got := binary.LittleEndian.Uint32(entry[12:]); got != wantPages {
			t.Fatalf("native memory directory %d pages = %d, want %d", i, got, wantPages)
		}
		if got := binary.LittleEndian.Uint32(entry[8:]); got != wantPages*65536 {
			t.Fatalf("native memory directory %d bytes = %d, want %d", i, got, wantPages*65536)
		}
	}
}

func TestStagedOwnedMultiMemorySnapshotRoundTrip(t *testing.T) {
	compiled := stagedMultiMemoryCompile(t, ownedMultiMemorySnapshotModule())
	snapshot, err := Capture(compiled, SnapshotOptions{Kind: SnapshotWarm, WarmFunc: "warm"})
	if err != nil {
		t.Fatalf("capture owned multi-memory snapshot: %v", err)
	}
	if got := len(snapshot.memories); got != 2 {
		t.Fatalf("captured memory count = %d, want 2", got)
	}
	if snapshot.memories[0].pages != 2 || snapshot.memories[1].pages != 3 {
		t.Fatalf("captured pages = %d,%d, want 2,3", snapshot.memories[0].pages, snapshot.memories[1].pages)
	}
	if got := string(snapshot.memories[1].image[32:36]); got != "snap" {
		t.Fatalf("captured memory.init bytes = %q, want snap", got)
	}
	if got := snapshotPassiveDataLens(snapshot); len(got) != 1 || got[0] != 0 {
		t.Fatalf("captured passive data lengths = %v, want [0]", got)
	}
	assertOwnedMultiMemorySnapshotState(t, snapshot)

	blob, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal owned multi-memory snapshot: %v", err)
	}
	if len(blob) >= (2+3)*65536 {
		t.Fatalf("zero-tail-trimmed snapshot blob = %d bytes, want less than full %d-byte memory images", len(blob), (2+3)*65536)
	}
	if _, err := LoadSnapshot(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("public multi-memory snapshot load error = %v, want fail-closed feature rejection", err)
	}
	loaded, err := loadStagedMultiMemorySnapshot(blob)
	if err != nil {
		t.Fatalf("staged load owned multi-memory snapshot: %v", err)
	}
	defer loaded.c.Close()
	if len(loaded.memories) != 2 || loaded.memories[0].pages != 2 || loaded.memories[1].pages != 3 {
		t.Fatalf("loaded memory metadata = %#v", loaded.memories)
	}
	if got := string(loaded.memories[1].image[32:36]); got != "snap" {
		t.Fatalf("loaded memory.init bytes = %q, want snap", got)
	}
	assertOwnedMultiMemorySnapshotState(t, loaded)
	t.Logf("snapshot-v%d blob=%d bytes Snapshot=%d memorySnap=%d", snapshotVersion, len(blob), unsafe.Sizeof(Snapshot{}), unsafe.Sizeof(memorySnap{}))
}

func TestStagedOwnedMultiMemorySnapshotRejectsMaliciousMetadata(t *testing.T) {
	compiled := stagedMultiMemoryCompile(t, ownedMultiMemorySnapshotModule())
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	makeBlob := func(memories ...memorySnap) []byte {
		out := append([]byte{}, snapshotMagic...)
		out = append(out, snapshotVersion, byte(SnapshotInit))
		out = binary.AppendUvarint(out, uint64(len(blob)))
		out = append(out, blob...)
		out = binary.AppendUvarint(out, uint64(len(memories)))
		for _, memory := range memories {
			out = binary.AppendUvarint(out, uint64(memory.pages))
			out = binary.AppendUvarint(out, uint64(len(memory.image)))
			out = append(out, memory.image...)
		}
		out = binary.AppendUvarint(out, 0) // globals
		out = binary.AppendUvarint(out, 1) // passive data length count
		out = binary.AppendUvarint(out, 4) // full/not dropped
		return out
	}
	for _, tc := range []struct {
		name string
		blob []byte
		want string
	}{
		{name: "memory count", blob: makeBlob(memorySnap{pages: 1}), want: "memory count 1 does not match"},
		{name: "page maximum", blob: makeBlob(memorySnap{pages: 4}, memorySnap{pages: 2}), want: "memory 0 page count 4 exceeds"},
		{name: "image exceeds pages", blob: makeBlob(memorySnap{pages: 1, image: make([]byte, 65537)}, memorySnap{pages: 2}), want: "memory 0 image length 65537 exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaded, err := loadStagedMultiMemorySnapshot(tc.blob)
			if loaded != nil {
				loaded.c.Close()
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("load malicious snapshot error = %v, want %q", err, tc.want)
			}
		})
	}
}
