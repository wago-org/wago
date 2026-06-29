package wasm

import "testing"

func TestTypeMetadataHelpersUseCanonicalFields(t *testing.T) {
	if got := GlobalValueType(GlobalType{Type: F32}); got != F32 {
		t.Fatalf("GlobalValueType = %s, want f32", got)
	}
	if got := TableRefType(TableType{Ref: ExternRef.Ref}); !EqualValType(RefVal(got), ExternRef) {
		t.Fatalf("TableRefType = %s, want externref", RefVal(got))
	}
	if got := TableAddrType(TableType{}); got != I32 {
		t.Fatalf("TableAddrType = %s, want i32", got)
	}
	if got := MemoryAddrType(MemType{Limits: Limits{Addr64: true}}); got != I64 {
		t.Fatalf("MemoryAddrType = %s, want i64", got)
	}
}

func TestLocalHelpersKeepRunsCompact(t *testing.T) {
	params := []ValType{I32}
	runs := []LocalRun{{Count: 1 << 30, Type: I64}, {Count: 2, Type: F32}}
	count, overflow := LocalCount(params, runs)
	if overflow || count != 1+(1<<30)+2 {
		t.Fatalf("LocalCount = %d/%v, want %d/false", count, overflow, uint64(1+(1<<30)+2))
	}
	if got, ok := LocalType(params, runs, 0); !ok || got != I32 {
		t.Fatalf("LocalType param = %s/%v, want i32", got, ok)
	}
	if got, ok := LocalType(params, runs, 1<<30); !ok || got != I64 {
		t.Fatalf("LocalType huge run tail = %s/%v, want i64", got, ok)
	}
	if got, ok := LocalType(params, runs, 1+(1<<30)); !ok || got != F32 {
		t.Fatalf("LocalType second run = %s/%v, want f32", got, ok)
	}
	if _, ok := LocalType(params, runs, uint32(count)); ok {
		t.Fatalf("LocalType out-of-range unexpectedly succeeded")
	}
}
