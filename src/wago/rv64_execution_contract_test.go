//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PortableRootedModule() []byte {
	callee := []byte{0, 0xfb, 1, 0, 0x1a, 0x20, 0, 0x0b}
	caller := []byte{1, 1, 0x7f, 0x41, 0x29, 0xfb, 0, 0, 0x41, 0x2a, 0xfb, 0, 0, 0x10, 0,
		0xfb, 2, 0, 0, 0x21, 0, 0xfb, 2, 0, 0, 0x20, 0, 0x6a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 1, 0x64, 0, 1, 0x64, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(callee))), callee...), append(wasmtest.ULEB(uint32(len(caller))), caller...))))
}

func TestRV64PortableRootedArtifactContract(t *testing.T) {
	path, mode := os.Getenv("WAGO_RV64_PORTABLE_ROOT_ARTIFACT"), os.Getenv("WAGO_RV64_PORTABLE_ROOT_ARTIFACT_MODE")
	var blob []byte
	var err error
	if mode == "load" {
		blob, err = os.ReadFile(path)
	} else {
		cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
		compiled, compileErr := Compile(cfg, rv64PortableRootedModule())
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		defer compiled.Close()
		blob, err = compiled.MarshalBinary()
		if err == nil && mode == "prepare" {
			err = os.WriteFile(path, blob, 0600)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	view := loaded.executionView()
	if !rv64ArtifactContractValid(view) || !rv64FreshRootedGC(view) || view.validateMemo.rv64FreshRootedGC {
		t.Fatal("loaded contract lost proof or restored a source-only trusted flag")
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if !rv64ArtifactContractValid(view) || !rv64FreshRootedGC(view) {
			panic("warm contract admission failed")
		}
	}); allocations != 0 {
		t.Fatalf("warm portable admission allocations=%g", allocations)
	}
	t.Logf("portable admission: 0 warm allocations; memo=%d Compiled=%d indexes=%d contract=%d bytes", unsafe.Sizeof(validateMemo{}), unsafe.Sizeof(Compiled{}), unsafe.Sizeof(compiledCacheIndexes{}), unsafe.Sizeof(rv64ExecutionContract{}))
	again, err := loaded.MarshalBinary()
	if err != nil || !slices.Equal(blob, again) {
		t.Fatalf("portable artifact differs after load: %v", err)
	}
	in, err := Instantiate(loaded, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for iteration := range 32 {
		if got, err := in.Invoke("run"); err != nil || !slices.Equal(got, []uint64{83}) {
			t.Fatalf("loaded run%d=%v,%v", iteration, got, err)
		}
	}
	for _, change := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"code", func(c *Compiled) { c.code = slices.Clone(c.code); c.code[len(c.code)-1] ^= 1 }},
		{"export", func(c *Compiled) { c.Exports = map[string]int{"renamed": 1} }},
		{"signature", func(c *Compiled) { c.Funcs[0].TypeIndex = 2 }},
		{"type key", func(c *Compiled) { c.FuncTypeID[0] ^= 1 }},
		{"entry", func(c *Compiled) { c.Entry[1] += 16; c.InternalEntry[1] += 16 }},
		{"Core1 policy", func(c *Compiled) { c.core1SegmentPreflight = !c.core1SegmentPreflight }},
		{"host slots", func(c *Compiled) { c.syncHostSlots++ }},
	} {
		t.Run(change.name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			change.mutate(candidate)
			if rv64ArtifactContractValid(candidate) || rv64FreshRootedGC(candidate) {
				t.Fatal("changed metadata retained portable contract")
			}
		})
	}
	bad := slices.Clone(blob)
	bad[len(bad)-1] ^= 1
	if broken, err := LoadTrustedArtifact(bad); err == nil {
		broken.Close()
		t.Fatal("changed contract digest admitted")
	}
	// Rebinding a checksum cannot repair structurally invalid frame ownership.
	candidate := cloneCompiledMetadata(view)
	rootMap := *candidate.genericGCFrameRoots()
	rootMap.callsites = slices.Clone(rootMap.callsites)
	rootMap.callsites[0].frameBytes = 1 << 20
	candidate.validateMemo = &validateMemo{gcFrameRoots: &rootMap, importModuleEnds: candidate.validateMemo.importModuleEnds}
	metadata, _, err := encodeCompiledMetadataBody(candidate, false, false)
	if err != nil {
		t.Fatal(err)
	}
	digest := rv64ExecutionDigest(candidate.code, metadata, candidate.syncHostSlots)
	metadata = append(metadata, rv64ExecutionContractMarker[:]...)
	metadata = append(metadata, 1, byte(candidate.syncHostSlots), byte(candidate.syncHostSlots>>8))
	metadata = append(metadata, digest[:]...)
	w := compiledWriter{}
	w.appendString(wagoMagic)
	w.u8(wagoVersion)
	w.u8(compiledHeader)
	w.section(compiledSectionCode, candidate.code)
	w.section(compiledSectionMetadata, metadata)
	if broken, err := LoadTrustedArtifact(w.buf); err == nil {
		broken.Close()
		t.Fatal("rebound invalid native frame admitted")
	} else if !strings.Contains(err.Error(), "frame or metadata ownership") {
		t.Fatalf("invalid frame error=%v", err)
	}
	t.Logf("portable contract: %d artifact bytes; metadata overhead=%d bytes; artifact path=%s", len(blob), rv64ExecutionContractBytes, filepath.Base(path))
}

func TestRV64StructuralCodecMetadataCannotExecute(t *testing.T) {
	metadata := Compiled{Entry: []int{0}, Funcs: []FuncSig{{}}, FuncTypeID: []uint64{0}, Exports: map[string]int{"dummy": 0}}
	input := newHandBuiltCompiled([]byte{0x13, 0, 0, 0}, metadata)
	defer input.Close()
	blob, err := input.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if _, _, err := loaded.Signature("dummy"); err != nil {
		t.Fatal(err)
	}
	if instance, err := Instantiate(loaded); err == nil {
		instance.Close()
		t.Fatal("inspection-only native image was executable")
	} else if !strings.Contains(err.Error(), "native-context trap ABI") {
		t.Fatalf("inspection-only execution error=%v", err)
	}
	input.nativeContextTrapABI = true
	blob, err = input.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadTrustedArtifact(blob); err == nil {
		loaded.Close()
		t.Fatal("declared native execution ABI bypassed load admission")
	}
}

func BenchmarkRV64PortableArtifactLoad(b *testing.B) {
	blob, err := os.ReadFile(os.Getenv("WAGO_RV64_PORTABLE_ROOT_ARTIFACT"))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(blob)))
	for range b.N {
		c, err := LoadTrustedArtifact(blob)
		if err != nil {
			b.Fatal(err)
		}
		if err := c.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRV64StateOnlyExceptionFrameMapIsEmpty(t *testing.T) {
	roots := &compiledGCFrameRoots{}
	c := &Compiled{requiredFeatures: CoreFeatureExceptionHandling, validateMemo: &validateMemo{gcFrameRoots: roots}}
	c.ensureCodeCache()
	c.codeCache.setNativeGCABIVersion(gc.NativeABIVersion)
	defer c.Close()
	if !rv64ArtifactFrameOwnership(c) {
		t.Fatal("state-only empty frame map rejected")
	}
	c.NumImports = 1
	if rv64ArtifactFrameOwnership(c) {
		t.Fatal("imported execution used state-only frame exception")
	}
	c.NumImports = 0
	roots.safepoints = []compiledGCFrameSafepoint{{id: 1, frameBytes: 48}}
	if rv64ArtifactFrameOwnership(c) {
		t.Fatal("state-only image acquired a native safepoint")
	}
	roots.safepoints = nil
	c.code = []byte{0x13, 0, 0, 0}
	if rv64ArtifactFrameOwnership(c) {
		t.Fatal("state-only frame exception admitted native code")
	}
}
