//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestRV64CompactExternExceptionEdges(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(fmt.Sprint(profile), func(t *testing.T) {
			param := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}
			in, tags := ownedExceptionTestInstance(t, profile, []ValueTypeDescriptor{param})
			in.c.requiredFeatures |= CoreFeatureExceptionHandling
			in.c.codeCache.setNativeGCABIVersion(gc.NativeABIVersion)
			in.c.validateMemo.gcFrameRoots = &compiledGCFrameRoots{}
			state, err := in.compactExternCollector(nil)
			if err != nil {
				t.Fatal(err)
			}
			collect := func(roots gc.RootSet) {
				t.Helper()
				if err := in.gc.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				drainExceptionReclaims(in.gc)
			}
			packet := make([]byte, ownedExceptionPayloadBytes)
			copy(packet, tags)
			leaf, err := in.gc.NewStructWithRoots(0, []gc.Value{gc.I32Value(42)}, gc.EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			word, err := state.externFromAny(leaf, gc.RefSliceRoots{leaf})
			if err != nil {
				t.Fatal(err)
			}
			binary.LittleEndian.PutUint64(packet[8:], word)
			exn := allocOwnedExceptionForTest(in, packet)
			collect(gc.RefSliceRoots{exn})
			ref, err := state.anyFromExtern(word, gc.RefSliceRoots{exn})
			if err != nil {
				t.Fatal(err)
			}
			value, err := in.gc.StructGet(ref, 0)
			if err != nil || value.Bits != 42 {
				t.Fatal("captured converted value", value, err)
			}
			collect(compactExternLooseTestRoots{in, word})
			if _, err := in.gc.ObjectType(exn); err == nil {
				t.Fatal("raw extern retained dead exception")
			}
			if _, err := state.anyFromExtern(word, compactExternLooseTestRoots{in, word}); err != nil {
				t.Fatal(err)
			}
			collect(gc.EmptyRoots{})
			if len(state.shells) != 0 {
				t.Fatal("dead exception retained converted shell")
			}

			token, err := in.refStore.issueExternref("captured foreign value")
			if err != nil {
				t.Fatal(err)
			}
			binary.LittleEndian.PutUint64(packet[8:], token)
			exn = allocOwnedExceptionForTest(in, packet)
			collect(gc.RefSliceRoots{exn})
			if in.refStore.releaseExternref(token) {
				t.Fatal("exception lost host extern pin")
			}
			collect(compactExternLooseTestRoots{in, token})
			if _, err := in.gc.ObjectType(exn); err == nil {
				t.Fatal("raw host extern retained dead exception")
			}
			if in.refStore.releaseExternref(token) {
				t.Fatal("raw host extern lost anchor")
			}
			collect(gc.EmptyRoots{})
			if !in.refStore.releaseExternref(token) || len(state.foreign) != 0 {
				t.Fatal("dead exception retained host extern anchor")
			}
		})
	}
}
