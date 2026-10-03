package wago

import (
	"bytes"
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func compactDataFixture() *Compiled {
	c := &Compiled{Data: make([]DataInit, minCompactActiveData)}
	for i := range c.Data {
		c.Data[i] = DataInit{Offset: OffsetInit{Base: uint32(i)}, Bytes: []byte{byte(i)}}
	}
	return c
}

func TestCompactExecutionDataOwnsExactPayloadAndBudget(t *testing.T) {
	if got := unsafe.Sizeof(compactDataRecord{}); got != 16 {
		t.Fatalf("compact data record size = %d, want16", got)
	}
	c := compactDataFixture()
	rounded := func(bytes uint64) uint64 { return bytes + min(bytes, 8192) }
	want := rounded(uint64(unsafe.Sizeof(Compiled{}))) + rounded(uint64(unsafe.Sizeof(compactActiveData{}))) + rounded(uint64(len(c.Data))*uint64(unsafe.Sizeof(compactDataRecord{}))) + uint64(len(c.Data))*2
	got, err := snapshotMetadataBytes(c, 0)
	if err != nil || got != want {
		t.Fatalf("budget=%d,%v; want%d", got, err, want)
	}
	if _, err := snapshotMetadataBytes(c, want-1); err == nil {
		t.Fatal("accepted undersized snapshot quota")
	}
	snapshot := cloneCompiledExecutionMetadata(c)
	if snapshot.compactData == nil || snapshot.Data != nil || snapshot.activeDataCount() != len(c.Data) {
		t.Fatal("execution snapshot did not compact data")
	}
	for i := range c.Data {
		d := snapshot.activeDataAt(i)
		if !reflect.DeepEqual(d, c.Data[i]) || cap(d.Bytes) != len(d.Bytes) {
			t.Fatalf("segment%d changed or exposes adjacent bytes", i)
		}
	}
	c.Data[0].Bytes[0] = 99
	c.Data[1].Offset.Base = 99
	if snapshot.activeDataAt(0).Bytes[0] != 0 || snapshot.activeDataAt(1).Offset.Base != 1 {
		t.Fatal("public edit reached snapshot")
	}
	mutable := cloneCompiledMetadata(snapshot)
	if mutable.compactData != nil || len(mutable.Data) != minCompactActiveData {
		t.Fatal("mutable clone did not expand private data")
	}
	mutable.Data[0].Bytes[0] = 88
	if snapshot.activeDataAt(0).Bytes[0] != 0 {
		t.Fatal("mutable clone aliases private payload")
	}
}

func TestCompactExecutionDataFallbackKeepsRareOffsets(t *testing.T) {
	for _, kind := range []string{"global", "expression", "empty-expression", "nil-payload"} {
		t.Run(kind, func(t *testing.T) {
			c := compactDataFixture()
			switch kind {
			case "global":
				c.Data[0].Offset = OffsetInit{HasGlobal: true, Global: 1}
			case "expression":
				c.Data[0].Offset.Expr = []byte{0x41, 0, 0x0b}
			case "empty-expression":
				c.Data[0].Offset.Expr = []byte{}
			case "nil-payload":
				c.Data[0].Bytes = nil
			}
			snapshot := cloneCompiledExecutionMetadata(c)
			if snapshot.compactData != nil || !reflect.DeepEqual(snapshot.Data, c.Data) {
				t.Fatal("fallback changed metadata")
			}
		})
	}
}

func compactDataModule(emptyOutOfBounds bool) []byte {
	segments := make([][]byte, minCompactActiveData)
	for i := range segments {
		offset := int32(i)
		value := []byte{byte(i + 17)}
		if emptyOutOfBounds && i == len(segments)-1 {
			offset = 65537
			value = []byte{}
		}
		d := []byte{0, 0x41}
		d = append(d, wasmtest.SLEB32(offset)...)
		d = append(d, 0x0b)
		d = append(d, wasmtest.ULEB(uint32(len(value)))...)
		d = append(d, value...)
		segments[i] = d
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x2d, 0, 0, 0x0b}))),
		wasmtest.Section(11, wasmtest.Vec(segments...)),
	)
}

func TestCompactExecutionDataCodecAndMutationIsolation(t *testing.T) {
	c, err := Compile(nil, compactDataModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.executionView().compactData == nil || len(c.Data) != minCompactActiveData {
		t.Fatal("public/execution data representations incorrect")
	}
	before, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	c.Data[0].Bytes[0] = 99
	c.Data[0].Offset.Base = 100
	c.Data = nil
	after, err := c.MarshalBinary()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("public mutation changed artifact:%v", err)
	}
	loaded := &Compiled{}
	if err := loaded.UnmarshalBinary(before); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	reencoded, err := loaded.MarshalBinary()
	if err != nil || !bytes.Equal(before, reencoded) {
		t.Fatalf("round trip changed artifact:%v", err)
	}
	for _, compiled := range []*Compiled{c, loaded} {
		in, err := Instantiate(compiled)
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("f")
		if err != nil || len(got) != 1 || got[0] != 17 {
			t.Fatalf("got%v,%v;want17", got, err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompactExecutionDataKeepsEmptySegmentBoundsCheck(t *testing.T) {
	c, err := Compile(nil, compactDataModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.executionView().compactData == nil {
		t.Fatal("fixture did not exercise compact representation")
	}
	if in, err := Instantiate(c); err == nil {
		in.Close()
		t.Fatal("empty out-of-bounds segment accepted")
	}
}

func TestCompactExecutionDataMatchesLegacyEncoding(t *testing.T) {
	for _, empty := range []bool{false, true} {
		c := compactDataFixture()
		for i := range c.Data {
			c.Data[i].MemoryIndex = uint32(i % 3)
			if empty || i%2 == 0 {
				c.Data[i].Bytes = []byte{}
			}
		}
		snapshot := cloneCompiledExecutionMetadata(c)
		if snapshot.compactData == nil {
			t.Fatal("fixture did not use compact directory")
		}
		var legacy, compact compiledWriter
		legacy.data(c.Data)
		compact.compiledData(snapshot)
		if !bytes.Equal(legacy.buf, compact.buf) {
			t.Fatal("compact directory changed legacy wire encoding")
		}
		for i := range c.Data {
			if !reflect.DeepEqual(c.Data[i], snapshot.activeDataAt(i)) {
				t.Fatalf("segment%d changed, empty=%v", i, empty)
			}
		}
	}
}

func TestCompactExecutionDataThreshold(t *testing.T) {
	c := compactDataFixture()
	if cloneCompiledExecutionMetadata(c).compactData == nil {
		t.Fatal("threshold did not compact")
	}
	c.Data = c.Data[:minCompactActiveData-1]
	if cloneCompiledExecutionMetadata(c).compactData != nil {
		t.Fatal("below threshold unexpectedly compacted")
	}
}
