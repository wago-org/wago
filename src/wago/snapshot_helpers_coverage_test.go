package wago

import (
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestSnapshotValidationAndReaderHelpers(t *testing.T) {
	if _, err := Capture(nil, SnapshotOptions{}); err == nil {
		t.Fatal("nil compiled module accepted for snapshot")
	}
	if _, err := Capture(&Compiled{boundsMode: BoundsChecksSignalsBased}, SnapshotOptions{}); err == nil {
		t.Fatal("signals-based module accepted for snapshot")
	}
	c := &Compiled{PassiveData: []PassiveDataInit{{Bytes: []byte("abc")}, {Bytes: nil}}}
	if got := compiledPassiveDataLens(c); len(got) != 2 || got[0] != 3 || got[1] != 0 {
		t.Fatalf("compiled passive lengths = %v", got)
	}
	if compiledPassiveDataLens(nil) != nil || compiledPassiveDataLens(&Compiled{}) != nil {
		t.Fatal("empty compiled passive lengths changed")
	}
	if got := snapshotPassiveDataLens(&Snapshot{c: c}); len(got) != 2 || got[0] != 3 {
		t.Fatalf("fallback passive lengths = %v", got)
	}
	if got := snapshotPassiveDataLens(&Snapshot{c: c, passiveDataLens: []uint32{0, 0}}); len(got) != 2 || got[0] != 0 {
		t.Fatalf("captured passive lengths = %v", got)
	}
	desc := make([]byte, len(c.PassiveData)*runtime.PassiveDataDescBytes)
	binary.LittleEndian.PutUint32(desc[8:], 3)
	if got := capturePassiveDataLens(&Instance{c: c, passiveDataDesc: desc}); len(got) != 2 || got[0] != 3 || got[1] != 0 {
		t.Fatalf("instance passive lengths = %v", got)
	}
	if capturePassiveDataLens(nil) != nil || capturePassiveDataLens(&Instance{c: c}) != nil {
		t.Fatal("empty passive descriptor handling changed")
	}
	for _, lens := range [][]uint32{{3, 0}, {0, 0}} {
		if err := validatePassiveDataLens(c, lens); err != nil {
			t.Fatalf("valid passive lengths %v: %v", lens, err)
		}
	}
	for _, lens := range [][]uint32{{}, {2, 0}} {
		if err := validatePassiveDataLens(c, lens); err == nil {
			t.Fatalf("invalid passive lengths accepted: %v", lens)
		}
	}
	if err := validatePassiveDataLens(nil, []uint32{1}); err == nil {
		t.Fatal("nil compiled accepted passive lengths")
	}
	if err := validateSnapshotModule(nil); err == nil {
		t.Fatal("nil snapshot module accepted")
	}
	if err := validateSnapshotModule(&Compiled{HasTable: true}); err == nil {
		t.Fatal("table snapshot module accepted")
	}
	if err := validateSnapshotModule(&Compiled{Globals: []GlobalDef{{Type: ValFuncRef}}}); err == nil {
		t.Fatal("reference global snapshot module accepted")
	}
	s := &Snapshot{c: c, kind: SnapshotWarm}
	if s.Module() != c || s.Kind() != SnapshotWarm {
		t.Fatal("snapshot metadata accessors changed")
	}
	if err := runWarm(&Instance{c: &Compiled{Exports: map[string]int{}}}, SnapshotOptions{}); err == nil {
		t.Fatal("warm snapshot accepted without an exported warm function")
	}

	if IsSnapshot(nil) || IsSnapshot([]byte("WGSN")) || !IsSnapshot([]byte("WGSN\x02\x00")) {
		t.Fatal("snapshot magic detection changed")
	}
	r := &snapReader{buf: []byte{0x02, 'a', 'b', 0x7f}}
	if got := string(r.sizedBytes("test")); got != "ab" || r.byte() != 0x7f || r.err != nil {
		t.Fatalf("snapshot reader = %q, %v", got, r.err)
	}
	for _, r := range []*snapReader{
		{buf: []byte{0x80}},
		{buf: []byte{0x05}},
		{buf: []byte{0x02, 0x01}},
	} {
		if r == nil {
			continue
		}
		if len(r.buf) == 1 && r.buf[0] == 0x80 {
			r.uvarint()
		} else if len(r.buf) == 1 {
			r.bytes(2)
		} else {
			r.count("items", 2)
		}
		if r.err == nil {
			t.Fatal("malformed snapshot reader input accepted")
		}
	}
}

func TestCaptureInstanceSnapshotCopiesLiveState(t *testing.T) {
	mem, err := NewMemory(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	mem.Bytes()[0] = 9
	c := &Compiled{PassiveData: []PassiveDataInit{{Bytes: []byte("abc")}}}
	g := &Global{Type: ValI32, cell: make([]byte, 8)}
	binary.LittleEndian.PutUint64(g.cell, 17)
	desc := make([]byte, runtime.PassiveDataDescBytes)
	binary.LittleEndian.PutUint32(desc[8:], 2)
	s := captureInstanceSnapshot(&Instance{c: c, memory: mem, globalCells: []*Global{g, nil}, passiveDataDesc: desc}, SnapshotOptions{Kind: SnapshotWarm})
	if s.c != c || s.Kind() != SnapshotWarm || s.memPages != 1 || s.memory[0] != 9 || s.globals[0].bits != 17 || s.globals[1].typ != 0 || s.passiveDataLens[0] != 2 {
		t.Fatalf("captured snapshot = %#v", s)
	}
	mem.Bytes()[0] = 0
	if s.memory[0] != 9 {
		t.Fatal("snapshot memory aliases live memory")
	}
}

func TestCaptureLocalMemorySnapshot(t *testing.T) {
	c, err := Compile(nil, wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{0, 1}))))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, err := Capture(c, SnapshotOptions{})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if s.Module() != c || len(s.memory) != 65536 || s.memPages != 1 {
		t.Fatalf("local-memory snapshot = %#v", s)
	}
	in, err := Instantiate(s)
	if err != nil {
		t.Fatalf("Instantiate snapshot: %v", err)
	}
	defer in.Close()
	if got := len(in.memory.Bytes()); got != 65536 {
		t.Fatalf("restored memory length = %d", got)
	}
}

func TestCaptureWarmLocalMemorySnapshot(t *testing.T) {
	c, err := Compile(nil, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("_start", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if s, err := Capture(c, SnapshotOptions{Kind: SnapshotWarm}); err != nil || s.Kind() != SnapshotWarm {
		t.Fatalf("warm Capture = %#v, %v", s, err)
	}
}

func TestCaptureRejectsImportedMemory(t *testing.T) {
	entry := append(wasmtest.Name("env"), wasmtest.Name("mem")...)
	entry = append(entry, 2, 0, 1) // memory import, min one wasm page
	c, err := Compile(nil, wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(entry))))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	mem, err := NewMemory(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	if _, err := Capture(c, SnapshotOptions{Imports: Imports{"env.mem": mem}}); err == nil {
		t.Fatal("host-imported memory accepted for snapshot")
	}
}

func TestSnapshotPortableBinaryAndFileRoundTrip(t *testing.T) {
	c, err := Compile(nil, wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	s := &Snapshot{
		c:        c,
		kind:     SnapshotWarm,
		memPages: 1,
		memory:   []byte{7, 0, 0},
		globals: []globalSnap{
			{typ: ValI32, bits: 12},
			{typ: ValV128, vec: V128{1, 2, 3}},
		},
	}
	blob, err := s.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	loaded, err := LoadSnapshot(blob)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if loaded.Kind() != SnapshotWarm || len(loaded.memory) != 65536 || loaded.memory[0] != 7 || len(loaded.globals) != 2 || loaded.globals[1].vec != (V128{1, 2, 3}) {
		t.Fatalf("loaded snapshot = %#v", loaded)
	}
	path := filepath.Join(t.TempDir(), "snapshot.bin")
	if err := s.WriteFile(path); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if fromFile, err := ReadSnapshotFile(path); err != nil || fromFile.memory[0] != 7 {
		t.Fatalf("ReadSnapshotFile = %#v, %v", fromFile, err)
	}
	if _, err := (*Snapshot)(nil).MarshalBinary(); err == nil {
		t.Fatal("nil snapshot marshaled")
	}
	if _, err := LoadSnapshot([]byte("not a snapshot")); err == nil {
		t.Fatal("non-snapshot blob loaded")
	}
}

func TestInstantiateSnapshotAndOptionForms(t *testing.T) {
	c, err := Compile(nil, wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := Instantiate(nil); err == nil {
		t.Fatal("nil instantiable source accepted")
	}
	if _, err := Instantiate(&Snapshot{}); err == nil {
		t.Fatal("unbound snapshot accepted")
	}
	in, err := Instantiate(&Snapshot{c: c}, nil)
	if err != nil {
		t.Fatalf("Instantiate snapshot: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	for _, opts := range []any{InstantiateOptions{}, &InstantiateOptions{}, Imports{}} {
		in, err := Instantiate(c, opts)
		if err != nil {
			t.Fatalf("Instantiate(%T): %v", opts, err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Instantiate(c, InstantiateOptions{}, InstantiateOptions{}); err == nil {
		t.Fatal("multiple option values accepted")
	}
	if _, err := Instantiate(c, 3); err == nil {
		t.Fatal("invalid option type accepted")
	}
}

func TestSnapshotWarmExecutionAndResolution(t *testing.T) {
	c, err := Compile(nil, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("_start", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := runWarm(in, SnapshotOptions{}); err != nil {
		t.Fatalf("default warm function: %v", err)
	}
	if err := runWarm(in, SnapshotOptions{WarmFunc: "_start"}); err != nil {
		t.Fatalf("explicit warm function: %v", err)
	}
	if err := runWarm(in, SnapshotOptions{WarmFunc: "missing"}); err == nil {
		t.Fatal("missing warm function accepted")
	}
}
