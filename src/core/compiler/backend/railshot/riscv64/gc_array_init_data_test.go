//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func canonicalSectionSizes(source []byte) []byte {
	canonical := append([]byte(nil), source[:8]...)
	for offset := 8; offset < len(source); {
		id := source[offset]
		offset++
		var size uint32
		for shift := uint(0); ; shift += 7 {
			b := source[offset]
			offset++
			size |= uint32(b&0x7f) << shift
			if b&0x80 == 0 {
				break
			}
		}
		canonical = append(canonical, id)
		canonical = binary.AppendUvarint(canonical, uint64(size))
		canonical = append(canonical, source[offset:offset+int(size)]...)
		offset += int(size)
	}
	return canonical
}

func TestRV64GCArrayInitDataExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCArrayInitDataHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCArrayInitDataWire(source) || !AdmitGCArrayInitDataSource(source) || !admitRV64GCArrayInitData(m) {
		t.Fatal("exact official array.init_data leader was not admitted")
	}
	canonical := canonicalSectionSizes(source)
	canonicalModule, err := wasm.DecodeModule(canonical)
	if err != nil || len(canonical) != 307 || !admitRV64GCArrayInitData(canonicalModule) {
		t.Fatalf("canonical alternate did not preserve decoded semantics: len=%d err=%v", len(canonical), err)
	}
	if admitRV64GCArrayInitDataWire(canonical) || AdmitGCArrayInitDataSource(canonical) {
		t.Fatal("semantically identical alternate wire encoding was admitted")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCArrayHelpers: true, SyncHostSlots: 6, GCArrayInitDataSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0xe3, 0x5f, 0xf5, 0x4a, 0xa5, 0xb4, 0x99, 0x1e, 0x8e, 0xa7, 0xf2, 0x38, 0x90, 0x2d, 0x15, 0x9b, 0x9f, 0x88, 0x1f, 0x74, 0x8e, 0xcb, 0x0d, 0xa7, 0xce, 0x70, 0x4e, 0x7c, 0x35, 0xf2, 0x25, 0xef}
	wantEntries := []int{0, 720, 1440, 2160, 2912, 3664}
	if len(compiled.Code) != 4824 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) {
		t.Fatalf("unexpected image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCArrayInitDataSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("decoded module compiled without its exact-wire witness")
	}
	for i := range source {
		candidate := append([]byte(nil), source...)
		candidate[i] ^= 1
		if admitRV64GCArrayInitDataWire(candidate) {
			t.Fatalf("wire mutation at byte %d was admitted", i)
		}
	}
}

func TestRV64GCArrayInitDataWideExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCArrayInitDataWideHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 435 || sha256.Sum256(source) != rv64GCArrayInitDataWideSourceDigest || !admitRV64GCArrayInitDataWire(source) || !AdmitGCArrayInitDataSource(source) || !admitRV64GCArrayInitDataWide(m) || admitRV64GCArrayInitDataPacked(m) {
		t.Fatalf("exact official wide array.init_data leader was not exclusively admitted: len=%d digest=%x want=%x wire=%t shape=%t anyshape=%t exported=%t wide=%t packed=%t", len(source), sha256.Sum256(source), rv64GCArrayInitDataWideSourceDigest, admitRV64GCArrayInitDataWire(source), admitExactProductWireShape(source, []byte{1, 3, 7, 12, 10, 11}), admitExactProductWireShape(source, nil), AdmitGCArrayInitDataSource(source), admitRV64GCArrayInitDataWide(m), admitRV64GCArrayInitDataPacked(m))
	}
	canonical := canonicalSectionSizes(source)
	canonicalModule, err := wasm.DecodeModule(canonical)
	if err != nil || len(canonical) != 412 || !admitRV64GCArrayInitDataWide(canonicalModule) {
		t.Fatalf("canonical alternate did not preserve decoded wide semantics: len=%d err=%v", len(canonical), err)
	}
	if admitRV64GCArrayInitDataWire(canonical) || AdmitGCArrayInitDataSource(canonical) {
		t.Fatal("semantically identical alternate wide wire encoding was admitted")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCArrayHelpers: true, SyncHostSlots: 6, GCArrayInitDataSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x43, 0x94, 0xfb, 0x40, 0xfd, 0x19, 0x9a, 0x68, 0x7a, 0xcb, 0xc0, 0xef, 0x67, 0xc1, 0x39, 0x0a, 0x8b, 0x5b, 0xea, 0x36, 0x44, 0x0a, 0xea, 0xe5, 0x47, 0x96, 0x0b, 0x23, 0xa9, 0x5d, 0x84, 0x9b}
	wantEntries := []int{0, 784, 1568, 2352, 3136, 3920, 4704, 5488, 6272, 7056, 7840, 8624}
	if len(compiled.Code) != 9960 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) {
		t.Fatalf("unexpected wide image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCArrayInitDataSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("decoded wide module compiled without its exact-wire witness")
	}
	for i := range source {
		candidate := append([]byte(nil), source...)
		candidate[i] ^= 1
		if admitRV64GCArrayInitDataWire(candidate) {
			t.Fatalf("wire mutation at byte %d was admitted", i)
		}
	}
}
