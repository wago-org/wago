package embedded32

import (
	"testing"
	"unsafe"
)

func TestStableHelperFrameLayouts(t *testing.T) {
	var table TableABI
	if unsafe.Offsetof(table.EntriesBase) != TableABIEntriesBaseOffset || unsafe.Offsetof(table.Length) != TableABILengthOffset || unsafe.Offsetof(table.Maximum) != TableABIMaximumOffset || unsafe.Offsetof(table.FunctionEntriesBase) != TableABIFunctionEntriesBaseOffset || unsafe.Offsetof(table.FunctionTypesBase) != TableABIFunctionTypesBaseOffset || unsafe.Sizeof(table) != TableABIBytes {
		t.Fatalf("TableABI layout entries=%d length=%d maximum=%d functions=%d types=%d size=%d", unsafe.Offsetof(table.EntriesBase), unsafe.Offsetof(table.Length), unsafe.Offsetof(table.Maximum), unsafe.Offsetof(table.FunctionEntriesBase), unsafe.Offsetof(table.FunctionTypesBase), unsafe.Sizeof(table))
	}

	var d DataSegmentABI
	if unsafe.Offsetof(d.Base) != DataSegmentBaseOffset || unsafe.Offsetof(d.Length) != DataSegmentLengthOffset || unsafe.Offsetof(d.Dropped) != DataSegmentDroppedOffset || unsafe.Sizeof(d) != DataSegmentABIBytes {
		t.Fatalf("DataSegmentABI layout base=%d length=%d dropped=%d size=%d", unsafe.Offsetof(d.Base), unsafe.Offsetof(d.Length), unsafe.Offsetof(d.Dropped), unsafe.Sizeof(d))
	}

	var f32 F32Frame
	if unsafe.Offsetof(f32.Op) != F32FrameOpOffset || unsafe.Offsetof(f32.ALo) != F32FrameALoOffset || unsafe.Offsetof(f32.AHi) != F32FrameAHiOffset || unsafe.Offsetof(f32.BLo) != F32FrameBLoOffset || unsafe.Offsetof(f32.BHi) != F32FrameBHiOffset || unsafe.Offsetof(f32.OutLo) != F32FrameOutLoOffset || unsafe.Offsetof(f32.OutHi) != F32FrameOutHiOffset || unsafe.Offsetof(f32.Trap) != F32FrameTrapOffset || unsafe.Sizeof(f32) != F32FrameBytes {
		t.Fatalf("F32Frame layout size=%d", unsafe.Sizeof(f32))
	}

	var i I64Frame
	checks := []struct {
		name      string
		got, want uintptr
	}{
		{"i64.op", unsafe.Offsetof(i.Op), I64FrameOpOffset},
		{"i64.aLo", unsafe.Offsetof(i.ALo), I64FrameALoOffset},
		{"i64.aHi", unsafe.Offsetof(i.AHi), I64FrameAHiOffset},
		{"i64.bLo", unsafe.Offsetof(i.BLo), I64FrameBLoOffset},
		{"i64.bHi", unsafe.Offsetof(i.BHi), I64FrameBHiOffset},
		{"i64.outLo", unsafe.Offsetof(i.OutLo), I64FrameOutLoOffset},
		{"i64.outHi", unsafe.Offsetof(i.OutHi), I64FrameOutHiOffset},
		{"i64.i32Out", unsafe.Offsetof(i.I32Out), I64FrameI32OutOffset},
		{"i64.trap", unsafe.Offsetof(i.Trap), I64FrameTrapOffset},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s offset=%d want=%d", c.name, c.got, c.want)
		}
	}
	if got := unsafe.Sizeof(i); got != I64FrameBytes {
		t.Fatalf("I64Frame size=%d want=%d", got, I64FrameBytes)
	}

	var f F64Frame
	checks = []struct {
		name      string
		got, want uintptr
	}{
		{"f64.op", unsafe.Offsetof(f.Op), F64FrameOpOffset},
		{"f64.aLo", unsafe.Offsetof(f.ALo), F64FrameALoOffset},
		{"f64.aHi", unsafe.Offsetof(f.AHi), F64FrameAHiOffset},
		{"f64.bLo", unsafe.Offsetof(f.BLo), F64FrameBLoOffset},
		{"f64.bHi", unsafe.Offsetof(f.BHi), F64FrameBHiOffset},
		{"f64.outLo", unsafe.Offsetof(f.OutLo), F64FrameOutLoOffset},
		{"f64.outHi", unsafe.Offsetof(f.OutHi), F64FrameOutHiOffset},
		{"f64.trap", unsafe.Offsetof(f.Trap), F64FrameTrapOffset},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s offset=%d want=%d", c.name, c.got, c.want)
		}
	}
	if got := unsafe.Sizeof(f); got != F64FrameBytes {
		t.Fatalf("F64Frame size=%d want=%d", got, F64FrameBytes)
	}

	var s SIMDABIFrame
	checks = []struct {
		name      string
		got, want uintptr
	}{
		{"simd.op", unsafe.Offsetof(s.Op), SIMDFrameOpOffset},
		{"simd.scalarLo", unsafe.Offsetof(s.ScalarLo), SIMDFrameScalarLoOffset},
		{"simd.scalarHi", unsafe.Offsetof(s.ScalarHi), SIMDFrameScalarHiOffset},
		{"simd.a", unsafe.Offsetof(s.A), SIMDFrameAOffset},
		{"simd.b", unsafe.Offsetof(s.B), SIMDFrameBOffset},
		{"simd.c", unsafe.Offsetof(s.C), SIMDFrameCOffset},
		{"simd.immediate", unsafe.Offsetof(s.Immediate), SIMDFrameImmediateOffset},
		{"simd.out", unsafe.Offsetof(s.Out), SIMDFrameOutOffset},
		{"simd.scalarOut", unsafe.Offsetof(s.ScalarOutLo), SIMDFrameScalarOutOffset},
		{"simd.memoryBase", unsafe.Offsetof(s.MemoryBase), SIMDFrameMemoryBaseOffset},
		{"simd.memoryLen", unsafe.Offsetof(s.MemoryLen), SIMDFrameMemoryLenOffset},
		{"simd.address", unsafe.Offsetof(s.Address), SIMDFrameAddressOffset},
		{"simd.lane", unsafe.Offsetof(s.Lane), SIMDFrameLaneOffset},
		{"simd.trap", unsafe.Offsetof(s.Trap), SIMDFrameTrapOffset},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s offset=%d want=%d", c.name, c.got, c.want)
		}
	}
	if got := unsafe.Sizeof(s); got != SIMDFrameBytes {
		t.Fatalf("SIMDABIFrame size=%d want=%d", got, SIMDFrameBytes)
	}
}

func TestRunSIMDABI(t *testing.T) {
	f := SIMDABIFrame{Op: 174, A: [4]uint32{1, 2, 3, 4}, B: [4]uint32{10, 20, 30, 40}}
	RunSIMDABI(&f)
	want := [4]uint32{11, 22, 33, 44}
	if f.Out != want {
		t.Fatalf("out=%v want=%v", f.Out, want)
	}
}
