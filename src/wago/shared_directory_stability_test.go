//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func sharedDirectoryHostFixture(primaryMax, secondaryFlags, secondaryMax byte) []byte {
	memories := [][]byte{{3, 1, primaryMax}}
	sizeIndex := byte(0)
	if secondaryFlags != 0 {
		memories = append(memories, []byte{secondaryFlags, 1, secondaryMax})
		sizeIndex = 1
	}
	exports := [][]byte{wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("memory", 2, 0)}
	if secondaryFlags != 0 {
		exports = append(exports, wasmtest.ExportEntry("secondary", 2, 1))
	}
	sizeOp := []byte{0x3f, sizeIndex}
	if secondaryFlags&4 != 0 {
		sizeOp = append(sizeOp, 0xa7)
	}
	body := []byte{0x41, 0, 0x20, 0, 0x36, 2, 0, 0x20, 0, 0x10, 0, 0x41, 0, 0x28, 2, 0, 0x6a}
	body = append(body, sizeOp...)
	body = append(body, 0x6a, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "host", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec(memories...)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestSharedDirectoryCallbackGrowthAndPublication(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		primaryMax, secondaryFlags, secondaryMax byte
	}{
		{"fixed", 1, 0, 0}, {"growable", 3, 0, 0}, {"fixed-pair", 1, 3, 1},
		{"growable-secondary", 1, 3, 3}, {"unshared-secondary", 1, 1, 1}, {"memory64-secondary", 1, 7, 1},
	} {
		for _, publish := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/publish=%t", tc.name, publish), func(t *testing.T) {
				if tc.secondaryFlags != 0 {
					requireNativeTestFeatures(t, CoreFeatureMultiMemory)
				}
				if tc.secondaryFlags&4 != 0 {
					requireNativeTestFeatures(t, CoreFeatureMemory64)
				}
				cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
				c, err := Compile(cfg, sharedDirectoryHostFixture(tc.primaryMax, tc.secondaryFlags, tc.secondaryMax))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				var in *Instance
				pages := uint32(1)
				grown := uint32(0)
				targetMax := uint32(tc.primaryMax)
				if tc.secondaryFlags != 0 {
					targetMax = uint32(tc.secondaryMax)
				}
				callback := func(v int32) int32 {
					target := in.memory
					if tc.secondaryFlags != 0 {
						target = in.memoryDir.memories[1]
					}
					done := make(chan struct{})
					go func() { grown = target.growShared(1, 0); close(done) }()
					<-done
					runtime.GC()
					if publish {
						if _, err := in.ExportedFunc("run"); err != nil {
							panic(HostTrap{Err: err})
						}
					}
					binary.LittleEndian.PutUint32(in.memory.UnsafeBytes(), uint32(v+5))
					return v * 3
				}
				in, err = Instantiate(c, testImports("env.host", callback))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if in.memoryDir == nil {
					t.Fatal("missing shared directory")
				}
				stable := tc.primaryMax == 1 && (tc.secondaryFlags == 0 || tc.secondaryFlags == 3 && tc.secondaryMax == 1)
				if in.memoryDir.sharedEntriesStable != stable {
					t.Fatalf("stable directory=%t want %t", in.memoryDir.sharedEntriesStable, stable)
				}

				for i := 0; i < 3; i++ {
					previous := pages
					canGrow := pages < targetMax && tc.secondaryFlags != 1
					if canGrow {
						pages++
					}
					got, err := in.Invoke("run", 7)
					if err != nil || len(got) != 1 || got[0] != 33+uint64(pages) {
						t.Fatalf("shared continuation=%v,%v pages=%d grown=%d backingPages=%d max=%d", got, err, pages, grown, in.memory.jm.CurrentPages(), in.memory.jm.MaxPages())
					}
					wantGrow := ^uint32(0)
					if canGrow {
						wantGrow = previous
					}
					if grown != wantGrow {
						t.Fatalf("growth=%d want %d", grown, wantGrow)
					}
				}
			})
		}
	}
}

func TestSharedDirectoryFixedCallbackCrossInstanceEntry(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := Compile(cfg, sharedDirectoryHostFixture(1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	innerCode, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x41, 9, 0x6a, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer innerCode.Close()
	inner, err := Instantiate(innerCode, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()
	var in *Instance
	callback := func(v int32) int32 {
		got, err := inner.Invoke("run", 1)
		if err != nil || len(got) != 1 || got[0] != 10 {
			panic(HostTrap{Err: fmt.Errorf("nested result=%v,%v", got, err)})
		}
		runtime.GC()
		binary.LittleEndian.PutUint32(in.memory.UnsafeBytes(), uint32(v+5))
		return v * 3
	}
	in, err = Instantiate(c, testImports("env.host", callback))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 3; i++ {
		got, err := in.Invoke("run", 7)
		if err != nil || len(got) != 1 || got[0] != 34 {
			t.Fatalf("reentry result=%v,%v", got, err)
		}
	}
}
