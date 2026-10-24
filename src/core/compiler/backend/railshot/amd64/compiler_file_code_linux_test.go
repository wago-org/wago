//go:build linux && amd64 && !tinygo

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestCompilerFilePolicyPreservesCallsAMD64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t,
		funcDef{nil, i32, []byte{0, 0x10, 1, 0x0b}},
		funcDef{nil, i32, []byte{0, 0x41, 42, 0x0b}},
	)
	var expected []byte
	for _, enabled := range []bool{false, true} {
		cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, Optimizations: map[string]bool{"file-backed-code": enabled, "inline": false}})
		if err != nil {
			t.Fatal(err)
		}
		defer cm.CodeImage.Close()
		if expected == nil {
			expected = append([]byte(nil), cm.Code...)
		} else if !bytes.Equal(expected, cm.Code) {
			t.Fatal("file policy changed native instructions")
		}
		if got := runCompiledAmd64u(t, cm); got != 42 {
			t.Fatalf("file policy %t: result=%d, want 42", enabled, got)
		}
	}
}

func TestCompilerFileCallAndLiteralReclaimAMD64(t *testing.T) {
	image, err := coreruntime.NewCompilerCodeBuffer(20 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if !image.OwnsCompilerFile() {
		t.Skip("temporary filesystem does not support compiler file staging")
	}
	if err := image.AppendZeros(20 << 20); err != nil {
		t.Fatal(err)
	}
	code := image.Bytes()
	entry := []int{0, 9 << 20, 18 << 20}
	island := len(code) - 16
	key := literalKey{lo: 0x0123456789abcdef, size: 8}
	binary.LittleEndian.PutUint64(code[island:], key.lo)
	want := append([]byte(nil), code...)
	relocs := make([][]callReloc, 3)
	var words []uint64
	offsets := []uint32{0}
	for i, base := range entry {
		target := (i + 1) % len(entry)
		relocs[i] = []callReloc{{at: 0, target: uint32(target)}}
		binary.LittleEndian.PutUint32(want[base:], uint32(int32(entry[target]-(base+4))))
		words = append(words, 1, key.lo, key.hi, uint64(key.size), uint64(4)<<32)
		offsets = append(offsets, uint32(len(words)))
		binary.LittleEndian.PutUint32(want[base+4:], uint32(int32(island-(base+8))))
	}
	if err := patchModuleRelocsWithImage(code, entry, entry, relocs, -1, [gcSharedStubMax]int{}, image); err != nil {
		t.Fatal(err)
	}
	if err := patchModuleLiteralRelocsWithImage(code, entry, words, offsets, island, []moduleLiteral{{key: key, off: 0}}, image); err != nil {
		t.Fatal(err)
	}
	if err := image.FinishCompilerCode(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(code, want) {
		t.Fatal("call/literal relocation or reclaimed image bytes changed")
	}
}
