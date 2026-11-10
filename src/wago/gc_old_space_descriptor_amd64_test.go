//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

var gcDescriptorSpaces = []struct {
	name string
	kind byte
}{
	{"Old", gc.NativeSpaceOld},
	{"Nursery", gc.NativeSpaceNursery},
	{"Large", gc.NativeSpaceLarge},
	{"Tiny", gc.NativeSpaceTiny},
}

func gcDescriptorConfig(space byte) GCConfig {
	cfg := GCConfig{NurseryBytes: 1 << 20}
	if space == gc.NativeSpaceLarge {
		cfg.LargeObjectBytes = 16
	}
	if space == gc.NativeSpaceTiny {
		cfg = GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}
	}
	return cfg
}

func gcDescriptorHandle(tb testing.TB, in *Instance, ref gc.Ref, wantSpace byte) []byte {
	tb.Helper()
	view := in.gc.NativeView()
	handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(uint32(ref)>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
	if got := handle[gc.NativeHandleSpaceOffset]; got != wantSpace {
		tb.Fatalf("actual space = %d, want %d", got, wantSpace)
	}
	return handle
}

func gcDescriptorInstance(tb testing.TB, c *Compiled, space byte) *Instance {
	tb.Helper()
	in, err := Instantiate(c, InstantiateOptions{GC: gcDescriptorConfig(space)})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { in.Close() })
	if in.c.genericGCFrameRoots() == nil || !in.gc.CollectsOnAllocation() {
		tb.Fatal("fixture requires exact native roots and the requested collector")
	}
	ref := gc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	if space == gc.NativeSpaceOld {
		if err := in.gc.ForcePromote(ref); err != nil {
			tb.Fatal(err)
		}
	}
	gcDescriptorHandle(tb, in, ref, space)
	return in
}

// Exercise the exact selected descriptor in every read resolver. In particular,
// a Large handle must not borrow the Old descriptor, despite shared normal
// backing. Restore corrupt metadata before checking the result or closing.
func TestGCResolverSelectedSpaceDescriptor(t *testing.T) {
	for _, name := range gcSpaceByteCases {
		t.Run(name, func(t *testing.T) {
			module, want := gcSpaceByteModule(name)
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, space := range gcDescriptorSpaces {
				t.Run(space.name, func(t *testing.T) {
					in := gcDescriptorInstance(t, c, space.kind)
					view := in.gc.NativeView()
					ref := gc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
					handle := gcDescriptorHandle(t, in, ref, space.kind)
					savedHandle := append([]byte(nil), handle...)
					savedView := *view
					for _, tc := range []struct {
						name string
						bad  bool
						edit func()
					}{
						{"selected-length", true, func() { view.Spaces[space.kind].Bytes = 0 }},
						{"other-length", false, func() {
							other := byte(gc.NativeSpaceOld)
							if space.kind == other {
								other = gc.NativeSpaceLarge
							}
							view.Spaces[other].Bytes = 0
						}},
						{"handle-count", true, func() { view.HandleCount = uint32(ref) >> 1 }},
						{"free", true, func() { handle[gc.NativeHandleSpaceOffset] = gc.NativeSpaceFree }},
						{"unknown-space", true, func() { handle[gc.NativeHandleSpaceOffset] = 255 }},
						{"offset-overflow", true, func() { binary.LittleEndian.PutUint32(handle[gc.NativeHandleOffsetOffset:], ^uint32(0)) }},
						{"size-overflow", true, func() { binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], ^uint32(0)) }},
						{"short-handle", true, func() { binary.LittleEndian.PutUint32(handle[gc.NativeHandleSizeOffset:], gc.HeaderSize-1) }},
					} {
						t.Run(tc.name, func(t *testing.T) {
							tc.edit()
							got, err := in.Invoke("run", 1)
							*view = savedView
							copy(handle, savedHandle)
							if tc.bad {
								var trap *TrapError
								if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
									t.Fatalf("got %v, %v; want cast trap", got, err)
								}
							} else if err != nil || len(got) != 1 || got[0] != uint64(want) {
								t.Fatalf("got %v, %v; want %d", got, err, want)
							}
						})
					}
					got, err := in.Invoke("run", 17)
					if err != nil || len(got) != 1 || got[0] != uint64(17*want) {
						t.Fatalf("restored read = %v, %v; want %d", got, err, 17*want)
					}
				})
			}
		})
	}
}

func benchmarkGCDescriptorLoop(b *testing.B, in *Instance, value uint32) {
	b.Helper()
	if got, err := in.Invoke("run", 17); err != nil || len(got) != 1 || got[0] != uint64(17*value) {
		b.Fatalf("warm read = %v, %v; want %d", got, err, 17*value)
	}
	if uint64(b.N) > uint64(^uint32(0)) {
		b.Fatalf("iteration count %d exceeds i32 guest-loop limit", b.N)
	}
	b.ReportAllocs()
	b.ResetTimer()
	got, err := in.Invoke("run", uint64(uint32(b.N)))
	b.StopTimer()
	want := uint64(uint32(uint64(b.N) * uint64(value)))
	if err != nil || len(got) != 1 || got[0] != want {
		b.Fatalf("run(%d) = %v, %v; want %d", b.N, got, err, want)
	}
}

func BenchmarkGCResolverSpaceDescriptor(b *testing.B) {
	for _, name := range gcSpaceByteCases {
		b.Run(name, func(b *testing.B) {
			module, want := gcSpaceByteModule(name)
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			for _, space := range gcDescriptorSpaces {
				b.Run(space.name, func(b *testing.B) {
					benchmarkGCDescriptorLoop(b, gcDescriptorInstance(b, c, space.kind), want)
				})
			}
		})
	}
}

// Every loop iteration consumes the previous iteration's loaded handle. The
// mutable local changes on each step, so an address cache cannot replace the
// physical resolver. An allocating export admits exact native roots.
func gcDescriptorDependentModule() []byte {
	typ := []byte{0x5f, 1, 0x6d, 1}
	global := []byte{0x6d, 1, 0xd0, 0x6d, 0xfb, 0, 0, 0x0b}
	body := []byte{
		2, 1, 0x6d, 1, 0x7f, // local 1 = eqref, local 2 = counter
		0x23, 0, 0x21, 1,
		0x02, 0x40, 0x03, 0x40,
		0x20, 2, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 1, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x21, 1,
		0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x0c, 0,
		0x0b, 0x0b,
		0x20, 2, 0x20, 1, 0xd1, 0x6a, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(typ, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("init", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code([]byte{0xd0, 0x6d, 0xfb, 0, 0, 0x24, 0, 0x0b}))),
	)
}

func gcDescriptorDependentInstance(tb testing.TB, c *Compiled, space byte) *Instance {
	tb.Helper()
	in := gcDescriptorInstance(tb, c, space)
	first := gc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	refs := gc.RefSliceRoots{first}
	for range 4095 {
		ref, err := in.gc.NewStructWithRoots(in.requireGCDomainType(0), []gc.Value{gc.RefValue(refs[len(refs)-1])}, refs)
		if err != nil {
			tb.Fatal(err)
		}
		refs = append(refs, ref)
	}
	if err := in.gc.StructSet(first, 0, gc.RefValue(refs[len(refs)-1])); err != nil {
		tb.Fatal(err)
	}
	for _, ref := range refs {
		if space == gc.NativeSpaceOld {
			if err := in.gc.ForcePromote(ref); err != nil {
				tb.Fatal(err)
			}
		}
		gcDescriptorHandle(tb, in, ref, space)
	}
	return in
}

func TestGCResolverDependentSpaceFixture(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDescriptorDependentModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, space := range gcDescriptorSpaces {
		t.Run(space.name, func(t *testing.T) {
			in := gcDescriptorDependentInstance(t, c, space.kind)
			for _, count := range []uint64{1, 4096, 8193} {
				got, err := in.Invoke("run", count)
				if err != nil || len(got) != 1 || got[0] != count {
					t.Fatalf("run(%d) = %v, %v", count, got, err)
				}
			}
		})
	}
}

func BenchmarkGCResolverDependentSpace(b *testing.B) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDescriptorDependentModule())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	for _, space := range gcDescriptorSpaces {
		b.Run(space.name, func(b *testing.B) {
			benchmarkGCDescriptorLoop(b, gcDescriptorDependentInstance(b, c, space.kind), 1)
		})
	}
}

// Build the same dependent ring, then promote a selected subset. Promotion
// occurs after all allocations, and does not collect the unpromoted nodes.
// Check every actual space byte after promotion; setup is outside the timer.
func gcDescriptorMixedInstance(tb testing.TB, c *Compiled, random bool) *Instance {
	tb.Helper()
	in := gcDescriptorDependentInstance(tb, c, gc.NativeSpaceNursery)
	first := gc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	refs := make([]gc.Ref, 4096)
	ref := first
	for i := range refs {
		refs[i] = ref
		value, err := in.gc.StructGet(ref, 0)
		if err != nil {
			tb.Fatal(err)
		}
		ref = value.Ref
	}
	if ref != first {
		tb.Fatal("mixed fixture is not a closed 4096-object ring")
	}
	spaces := make([]byte, len(refs))
	state := uint32(0x9e3779b9)
	oldCount := 0
	for i, ref := range refs {
		old := i%2 == 0
		if random {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			old = state&1 != 0
		}
		spaces[i] = gc.NativeSpaceNursery
		if old {
			if err := in.gc.ForcePromote(ref); err != nil {
				tb.Fatal(err)
			}
			spaces[i] = gc.NativeSpaceOld
			oldCount++
		}
	}
	if oldCount < 1024 || oldCount > 3072 {
		tb.Fatalf("mixed fixture Old count = %d, want 25–75%%", oldCount)
	}
	for i, ref := range refs {
		gcDescriptorHandle(tb, in, ref, spaces[i])
	}
	return in
}

func TestGCResolverMixedSpaceFixture(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDescriptorDependentModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, random := range []bool{false, true} {
		in := gcDescriptorMixedInstance(t, c, random)
		for _, count := range []uint64{1, 4096, 8193} {
			got, err := in.Invoke("run", count)
			if err != nil || len(got) != 1 || got[0] != count {
				t.Fatalf("random=%v run(%d) = %v, %v", random, count, got, err)
			}
		}
	}
}

func BenchmarkGCResolverMixedSpace(b *testing.B) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDescriptorDependentModule())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	for _, tc := range []struct {
		name   string
		random bool
	}{{"Alternating", false}, {"Random", true}} {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkGCDescriptorLoop(b, gcDescriptorMixedInstance(b, c, tc.random), 1)
		})
	}
}
