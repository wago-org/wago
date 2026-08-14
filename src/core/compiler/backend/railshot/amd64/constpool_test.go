//go:build amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestConstPoolUsesFlatReusableStorageAMD64(t *testing.T) {
	f := fn{
		transient: transient{
			v128Pool:  make([]poolConst, 0, 2),
			poolSites: make([]poolSite, 0, 3),
		},
	}
	a := [16]byte{1, 2, 3, 4}
	b := [16]byte{1, 2, 3, 4, 5}
	allocs := testing.AllocsPerRun(100, func() {
		f.v128Pool = f.v128Pool[:0]
		f.poolSites = f.poolSites[:0]
		f.recordConst(a[:4], 11)
		f.recordConst(b[:8], 22)
		f.recordConst(a[:4], 33)
	})
	if allocs != 0 {
		t.Fatalf("recording into retained pool storage allocated %.1f times", allocs)
	}
	if got := len(f.v128Pool); got != 2 {
		t.Fatalf("distinct constants = %d, want 2", got)
	}
	if got := len(f.poolSites); got != 3 {
		t.Fatalf("flat sites = %d, want 3", got)
	}
	first := f.v128Pool[0]
	if first.size != 4 || first.head == 0 {
		t.Fatalf("first constant = %#v", first)
	}
	latest := f.poolSites[first.head-1]
	older := f.poolSites[latest.next-1]
	if latest.off != 33 || older.off != 11 || older.next != 0 {
		t.Fatalf("site chain = %#v -> %#v, want 33 -> 11", latest, older)
	}
}

func TestConstPoolAttributesLiteralBytesAMD64(t *testing.T) {
	f := fn{a: &encoderamd64.Asm{B: make([]byte, 4)}, stats: &CodegenStats{}}
	f.recordConst([]byte{1, 2, 3, 4}, 0)
	f.emitV128ConstPool()
	if got := f.stats.NativeSize.LiteralPoolBytes; got != 4 {
		t.Fatalf("literal bytes = %d, want 4", got)
	}
	if got := len(f.a.B); got != 8 {
		t.Fatalf("code plus pool bytes = %d, want 8", got)
	}
}

func TestModuleLiteralLedgerCountsCrossFunctionDuplicatesAMD64(t *testing.T) {
	key := literalKey{lo: 0x04030201, size: 4}
	stats := ModuleStats{Funcs: []*CodegenStats{
		{NativeSize: NativeFunctionSizeReport{TotalBytes: 4, InternalFunctionBytes: 4, LiteralPoolBytes: 4}, literalKeys: []literalKey{key}},
		{NativeSize: NativeFunctionSizeReport{TotalBytes: 4, InternalFunctionBytes: 4, LiteralPoolBytes: 4}, literalKeys: []literalKey{key}},
	}}
	finalizeModuleNativeSizeAMD64(&stats, 8, 8, 0, 0)
	if got, want := stats.NativeSize.LiteralPoolUniqueBytes, 4; got != want {
		t.Fatalf("unique literal bytes = %d, want %d", got, want)
	}
	if got, want := stats.NativeSize.LiteralPoolDuplicateBytes, 4; got != want {
		t.Fatalf("duplicate literal bytes = %d, want %d", got, want)
	}
}

func TestSizeObjectiveSharesModuleLiteralsAMD64(t *testing.T) {
	want := math.Float64bits(3.75)
	body := []byte{0x00, 0x44} // f64.const 1.5
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(1.5))
	body = append(body, 0x44) // f64.const 2.25
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(2.25))
	body = append(body, 0xa0, 0xbd, 0x0b) // f64.add; i64.reinterpret_f64; end
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I64}, body: body},
		funcDef{results: []wasm.ValType{wasm.I64}, body: body},
	)

	var balancedStats ModuleStats
	balanced, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &balancedStats})
	if err != nil {
		t.Fatal(err)
	}
	if balanced.CodeImage != nil {
		defer balanced.CodeImage.Close()
	}
	if got, unique, duplicate := balancedStats.NativeSize.LiteralPoolBytes, balancedStats.NativeSize.LiteralPoolUniqueBytes, balancedStats.NativeSize.LiteralPoolDuplicateBytes; got != 32 || unique != 16 || duplicate != 16 {
		t.Fatalf("Balanced literals = physical:%d unique:%d duplicate:%d, want 32/16/16", got, unique, duplicate)
	}

	objective := OptimizeSize
	beforeIsland := moduleLiteralIslandEnabled
	moduleLiteralIslandEnabled = false
	t.Cleanup(func() { moduleLiteralIslandEnabled = beforeIsland })
	var rollbackStats ModuleStats
	rollback, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Workers: 1, Stats: &rollbackStats})
	if err != nil {
		t.Fatal(err)
	}
	if rollback.CodeImage != nil {
		defer rollback.CodeImage.Close()
	}
	if got := rollbackStats.NativeSize.LiteralPoolBytes; got != 32 {
		t.Fatalf("Size rollback literal bytes = %d, want 32", got)
	}
	moduleLiteralIslandEnabled = true
	var sizeStats ModuleStats
	sizeSerial, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Workers: 1, Stats: &sizeStats})
	if err != nil {
		t.Fatal(err)
	}
	if sizeSerial.CodeImage != nil {
		defer sizeSerial.CodeImage.Close()
	}
	sizeParallel, err := CompileModuleWith(m, CompileOptions{Objective: &objective, Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sizeSerial.Code, sizeParallel.Code) {
		t.Fatal("Size literal-island output differs between serial and parallel compilation")
	}
	if got, unique, duplicate := sizeStats.NativeSize.LiteralPoolBytes, sizeStats.NativeSize.LiteralPoolUniqueBytes, sizeStats.NativeSize.LiteralPoolDuplicateBytes; got != 16 || unique != 16 || duplicate != 0 {
		t.Fatalf("Size literals = physical:%d unique:%d duplicate:%d, want 16/16/0", got, unique, duplicate)
	}
	if got := runCompiledAmd64u(t, sizeSerial); got != want {
		t.Fatalf("shared literal execution = %#x, want %#x", got, want)
	}
}
