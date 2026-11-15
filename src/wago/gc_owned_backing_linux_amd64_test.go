// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Inspect the private owner flag without adding a production diagnostic API.
func mappedGCBacking(c *gc.Collector) bool {
	return reflect.ValueOf(c).Elem().FieldByName("throughput").FieldByName("mapped").Bool()
}

func mappedGCHandleBacking(c *gc.Collector) bool {
	// State2 is the private mapped-handle owner; no production API is added.
	return reflect.ValueOf(c).Elem().FieldByName("handlesBacking").Uint() == 2
}

func ownedGCBackingModule() []byte {
	host := append(wasmtest.Name("host"), wasmtest.Name("inspect")...)
	host = append(host, 0, 1)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(host)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0, 0xfb, 0, 0, 0x24, 0, // global = new struct(argument)
			0x20, 0, 0x10, 0, 0x1a, // host.inspect(argument); drop
			0x23, 0, 0xfb, 2, 0, 0, 0x0b, // read global after the host callback closes the instance
		}))),
	)
}

func TestMappedGCBackingSurvivesBorrowedClose(t *testing.T) {
	for _, shared := range []bool{false, true} {
		name := "private"
		if shared {
			name = "shared"
		}
		t.Run(name, func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit)
			heap := GCConfig{LargeObjectBytes: 16, ThroughputHeapBytes: 1 << 20}
			var in *Instance
			var callback bool
			host := slotHostFunc(func(m HostModule, args, results []uint64) {
				err := m.(GuestStorageHostModule).WithGuestStorage(func(GuestStorage) error {
					callback = true
					if !mappedGCBacking(in.gc) || !mappedGCHandleBacking(in.gc) {
						return &guestStorageTestError{"callback did not reach mapped old space"}
					}
					ref := gc.Ref(uint32(readGlobalObject(in.globalCells[0], ValAnyRef)))
					expected := gc.TypeID(0)
					if in.gcTypeMap != nil {
						expected = in.gcTypeMap.localToDomain[0]
					}
					payload, err := in.gc.StructPayloadReadOnly(ref, expected)
					if err != nil {
						return err
					}
					if err = in.Close(); err != nil {
						return err
					}
					if !in.hasPhysicalResources() || !mappedGCBacking(in.gc) || !mappedGCHandleBacking(in.gc) {
						return &guestStorageTestError{"Close released backing during active borrow"}
					}
					if binary.LittleEndian.Uint32(payload) != 41 {
						t.Error("borrowed payload changed after logical close")
					}
					results[0] = args[0]
					return nil
				})
				if err != nil {
					panic(HostTrap{Err: err})
				}
			})
			if shared {
				rt := NewRuntime(WithRuntimeConfig(cfg))
				defer rt.Close()
				mod, err := rt.Compile(ownedGCBackingModule())
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				in, err = rt.Instantiate(context.Background(), mod, WithImports(testImports("host.inspect", host)), WithGC(heap))
				if err != nil {
					t.Fatal(err)
				}
			} else {
				compiled, err := Compile(cfg, ownedGCBackingModule())
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				in, err = Instantiate(compiled, InstantiateOptions{Imports: testImports("host.inspect", host), GC: heap})
				if err != nil {
					t.Fatal(err)
				}
			}
			defer in.Close()
			released := make(chan struct{}, 1)
			in.referenceLifetime().afterPhysicalRelease(func() { released <- struct{}{} })
			out, err := in.Invoke("run", 41)
			var trap *coreruntime.TrapError
			if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted || !callback {
				t.Fatalf("run=%v error=%v callback=%v", out, err, callback)
			}
			select {
			case <-released:
			case <-time.After(5 * time.Second):
				t.Fatal("guest completion did not finish physical release")
			}
			if in.hasPhysicalResources() || mappedGCBacking(in.gc) || mappedGCHandleBacking(in.gc) {
				t.Fatal("owner retained mapped backing after physical release")
			}
		})
	}
}

func TestMappedGCBackingSharedSiblingOwnsReservation(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	mod, err := rt.Compile(gcLifecycleModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	heap := GCConfig{LargeObjectBytes: 16, ThroughputHeapBytes: 1 << 20}
	first, err := rt.Instantiate(context.Background(), mod, WithGC(heap))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := rt.Instantiate(context.Background(), mod, WithGC(heap))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.gc != second.gc {
		t.Fatal("instances did not share the tested collector")
	}
	if _, err = first.Invoke("set", 41); err != nil {
		t.Fatal(err)
	}
	collector := first.gc
	if !mappedGCBacking(collector) || !mappedGCHandleBacking(collector) {
		t.Fatal("first call did not reserve mapped old space")
	}
	firstDone := make(chan struct{}, 1)
	first.referenceLifetime().afterPhysicalRelease(func() { firstDone <- struct{}{} })
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first physical release did not finish")
	}
	if !mappedGCBacking(collector) || !mappedGCHandleBacking(collector) {
		t.Fatal("first close released a live sibling's collector")
	}
	if got, err := second.Invoke("set", 42); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("live sibling=%v error=%v", got, err)
	}
	secondDone := make(chan struct{}, 1)
	second.referenceLifetime().afterPhysicalRelease(func() { secondDone <- struct{}{} })
	if err = second.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondDone:
	case <-time.After(5 * time.Second):
		t.Fatal("last physical release did not finish")
	}
	if mappedGCBacking(collector) || mappedGCHandleBacking(collector) {
		t.Fatal("last close retained the shared reservation")
	}
}
