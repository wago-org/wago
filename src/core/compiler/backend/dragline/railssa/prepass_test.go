package railssa

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestPrepassScratchRecordsStayDense(t *testing.T) {
	if got := unsafe.Sizeof(regionLocalEvent(0)); got != 8 {
		t.Fatalf("region-local event size = %d, want 8", got)
	}
}

func TestStructuredPrepassRecordsRegionsAndLocalFlow(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, nil, []byte{
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, // local.get 0
		0x41, 0x01, // i32.const 1
		0x6b,       // i32.sub
		0x22, 0x00, // local.tee 0
		0x0d, 0x00, // br_if loop
		0x0b,       // end loop
		0x0b,       // end block
		0x20, 0x00, // local.get 0 after merge
		0x1a, // drop
		0x0b, // function end
	})
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Regions) != 2 || f.MaxLoopDepth != 1 {
		t.Fatalf("regions=%d maxLoopDepth=%d", len(f.Regions), f.MaxLoopDepth)
	}
	block, loop := f.Regions[0], f.Regions[1]
	if block.Parent != NoRegion || block.Kind != wasm.InstrBlock || block.StartInstr != 0 || block.EndInstr != 8 {
		t.Fatalf("block region = %#v", block)
	}
	if loop.Parent != 0 || loop.Kind != wasm.InstrLoop || loop.StartInstr != 1 || loop.EndInstr != 7 || loop.LoopDepth != 1 {
		t.Fatalf("loop region = %#v", loop)
	}
	if !reflect.DeepEqual(block.AssignedLocals(f), []uint32{0}) || !reflect.DeepEqual(block.MergeLocals(f), []uint32{0}) || len(block.LoopLocals(f)) != 0 {
		t.Fatalf("block locals assigned=%v merge=%v loop=%v", block.AssignedLocals(f), block.MergeLocals(f), block.LoopLocals(f))
	}
	if !reflect.DeepEqual(loop.AssignedLocals(f), []uint32{0}) || !reflect.DeepEqual(loop.MergeLocals(f), []uint32{0}) || !reflect.DeepEqual(loop.LoopLocals(f), []uint32{0}) {
		t.Fatalf("loop locals assigned=%v merge=%v loop=%v", loop.AssignedLocals(f), loop.MergeLocals(f), loop.LoopLocals(f))
	}
	if block.MaxPressure < 2 || loop.MaxPressure < 2 || f.BuildPeakBytes < f.CapacityBytes() {
		t.Fatalf("pressure/build bytes block=%d loop=%d peak=%d retained=%d", block.MaxPressure, loop.MaxPressure, f.BuildPeakBytes, f.CapacityBytes())
	}
}

func TestStructuredPrepassRecordsIfElseBoundaries(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, nil, []byte{
		0x20, 0x00,
		0x04, 0x40,
		0x41, 0x01, 0x21, 0x00,
		0x05,
		0x41, 0x02, 0x21, 0x00,
		0x0b,
		0x20, 0x00, 0x1a,
		0x0b,
	})
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Regions) != 1 {
		t.Fatalf("regions = %d", len(f.Regions))
	}
	region := f.Regions[0]
	if region.Kind != wasm.InstrIf || region.StartInstr != 1 || region.ElseInstr != 4 || region.EndInstr != 7 {
		t.Fatalf("if region = %#v", region)
	}
	if !reflect.DeepEqual(region.AssignedLocals(f), []uint32{0}) || !reflect.DeepEqual(region.MergeLocals(f), []uint32{0}) {
		t.Fatalf("if locals assigned=%v merge=%v", region.AssignedLocals(f), region.MergeLocals(f))
	}
}

func TestCompactPrepassPreservesNestedRegionLocalFacts(t *testing.T) {
	body := []byte{
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, 0x1a, // local.get 0; drop
		0x41, 0x01, 0x21, 0x00, // i32.const 1; local.set 0
		0x0b, // end loop
		0x0b, // end block
		0x0b, // function end
	}
	largeBody := append([]byte(nil), body[:len(body)-1]...)
	padding := make([]byte, compactPrepassBodyBytes)
	for index := range padding {
		padding[index] = 0x01 // nop
	}
	largeBody = append(largeBody, padding...)
	largeBody = append(largeBody, 0x0b)
	small, err := BuildStackFunc(scalarModule([]wasm.ValType{wasm.I32}, nil, body), 0)
	if err != nil {
		t.Fatal(err)
	}
	large, err := BuildStackFunc(scalarModule([]wasm.ValType{wasm.I32}, nil, largeBody), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(small.RegionLocals, large.RegionLocals) || !reflect.DeepEqual(small.MergeLocals, large.MergeLocals) || !reflect.DeepEqual(small.LoopLocals, large.LoopLocals) {
		t.Fatalf("compact locals assigned=%v merge=%v loop=%v; want assigned=%v merge=%v loop=%v", large.RegionLocals, large.MergeLocals, large.LoopLocals, small.RegionLocals, small.MergeLocals, small.LoopLocals)
	}
}
