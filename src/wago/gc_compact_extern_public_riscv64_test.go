//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"context"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64CompactExternPublicTokenLifetime(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x41, 42, 0x0b}))),
	)
	c, err := NewRuntimeConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for _, candidate := range []*Compiled{c, loaded} {
		rt := NewRuntime()
		in := rv64NativeTokenInstantiate(t, rt, candidate, NewImports())
		domain := in.lockGCCollector()
		state, err := in.compactExternCollector(domain)
		if err != nil {
			t.Fatal(err)
		}
		word, err := state.externFromAny(gc.Ref(15), gc.EmptyRoots{})
		unlockGCCollector(domain)
		if err != nil {
			t.Fatal(err)
		}
		externType := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}
		bad := []uint64{word, 0}
		if err := in.translatePublicReferenceResults("compact rollback", bad, []ValType{ValExternRef, ValFuncRef}, []ValueTypeDescriptor{externType, {Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapFunc}}}}); err == nil {
			t.Fatal("later invalid result did not reject converted extern batch")
		}
		if public := in.existingPublicGCState(); public.resultTokenCount != 0 {
			t.Fatal("failed result batch retained its converted extern root")
		}
		for range 128 {
			token, err := in.issueCompactExternPublicToken(word)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := rt.ExternRefValue(ExternRef{token}); !ok {
				t.Fatal("public converted extern token did not resolve")
			}
			if !rt.ReleaseExternRef(ExternRef{token}) || rt.ReleaseExternRef(ExternRef{token}) {
				t.Fatal("public converted extern token release/stale proof")
			}
		}
		if public := in.existingPublicGCState(); public.resultRootsMade != 1 || public.resultTokenCount != 0 {
			t.Fatalf("public extern roots grew after reuse: made=%d live=%d", public.resultRootsMade, public.resultTokenCount)
		}
		token, err := in.issueCompactExternPublicToken(word)
		if err != nil {
			t.Fatal(err)
		}
		in.Close()
		if !in.hasPhysicalResources() {
			t.Fatal("public extern token lost its closed producer")
		}
		if !rt.ReleaseExternRef(ExternRef{token}) || in.hasPhysicalResources() {
			t.Fatal("released public extern token retained its closed producer")
		}
		rt.Close()
		if compactExternWordReserved(in.refStore, word) {
			t.Fatal("closed collector retained a converted extern identity")
		}
		shutdown := NewRuntime()
		live := rv64NativeTokenInstantiate(t, shutdown, candidate, NewImports())
		domain = live.lockGCCollector()
		state, err = live.compactExternCollector(domain)
		if err != nil {
			t.Fatal(err)
		}
		shutdownWord, err := state.externFromAny(gc.Ref(15), gc.EmptyRoots{})
		unlockGCCollector(domain)
		if err != nil {
			t.Fatal(err)
		}
		shutdownToken, err := live.issueCompactExternPublicToken(shutdownWord)
		if err != nil {
			t.Fatal(err)
		}
		live.Close()
		if !live.hasPhysicalResources() {
			t.Fatal("live extern token lost its owner before Runtime close")
		}
		if err := shutdown.CloseContext(context.Background()); err != nil {
			t.Fatal(err)
		}
		if live.hasPhysicalResources() || compactExternWordReserved(live.refStore, shutdownWord) || shutdown.ReleaseExternRef(ExternRef{shutdownToken}) {
			t.Fatal("Runtime close retained the extern token root or identity")
		}

		t.Logf("public extern receipt=%d Go bytes; extern slot=%d bytes; reused native root slots=1", unsafe.Sizeof(compactExternPublicToken{}), unsafe.Sizeof(externrefSlot{}))
	}
}
