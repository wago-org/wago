//go:build linux && amd64 && (wago_gc_compact_reads || (wago_gc_compact_alloc || wago_gc_compact_refstore))

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"testing"
	"unsafe"
)

func TestCompactNativeGCReadValidation(t *testing.T) {
	for _, kind := range []string{"null", "i31", "unaligned", "interior", "instance-version", "instance-size", "type-count", "collector-version", "collector-size", "arena-extent", "start-count", "start-byte", "header-type", "active-extent", "stride-extent", "inactive-region"} {
		t.Run(kind, func(t *testing.T) {
			data := compactPublicRootsModule()
			c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			rt := NewRuntime()
			defer rt.Close()
			in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}})
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			view := in.gc.CompactView()
			saved := *view
			iv := *in.compactState.view
			defer func() { *view = saved; *in.compactState.view = iv }()
			var restore func()
			if kind != "null" {
				values, e := in.InvokeValues(context.Background(), "new", ValueI32(91))
				if e != nil {
					t.Fatal(e)
				}
				defer in.ReleaseGCRef(values[0].GCRef())
				if e = in.SetGlobalValue("root", values[0]); e != nil {
					t.Fatal(e)
				}
				entry := in.refStore.gcByToken[values[0].GCRef().token]
				ref := in.gc.GlobalSlot(entry.slot)
				view = in.gc.CompactView()
				saved = *view
				arena := unsafe.Slice((*byte)(unsafe.Pointer(view.Arena)), int(view.ArenaBytes))
				starts := unsafe.Slice((*byte)(unsafe.Pointer(view.Starts)), int(view.StartCount))
				switch kind {
				case "i31", "unaligned", "interior":
					original := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
					bad := uint64(3)
					if kind == "unaligned" {
						bad = uint64(ref) + 2
					}
					if kind == "interior" {
						bad = uint64(ref) + 16
					}
					writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, bad)
					restore = func() { writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, original) }
				case "instance-version":
					in.compactState.view.Version++
				case "instance-size":
					in.compactState.view.Size = 0
				case "type-count":
					in.compactState.view.LocalTypeCount = 0
				case "collector-version":
					view.Version++
				case "collector-size":
					view.Size = 0
				case "arena-extent":
					view.ArenaBytes = uint32(ref) + 4 - 1
				case "start-count":
					view.StartCount = uint32(ref) / 8
				case "start-byte":
					old := starts[uint32(ref)/8]
					starts[uint32(ref)/8] = 0
					restore = func() { starts[uint32(ref)/8] = old }
				case "active-extent":
					view.NurseryBump = uint32(ref) + 3
				case "stride-extent":
					view.NurseryBump = uint32(ref) + 15
				case "header-type":
					offset := uint32(ref)
					old := binary.LittleEndian.Uint32(arena[offset:])
					v := ^uint32(0)
					binary.LittleEndian.PutUint32(arena[offset:], v)
					restore = func() { binary.LittleEndian.PutUint32(arena[offset:], old) }
				case "inactive-region":
					view.NurseryBump = view.NurseryStart
					view.OldBump = view.OldStart
				}
			}
			if restore != nil {
				defer restore()
			}
			_, e = in.Invoke("read")
			var trap *TrapError
			if !errors.As(e, &trap) {
				t.Fatalf("missing checked native trap: %v", e)
			}
			if kind == "null" {
				m, e := wasm.DecodeModule(data)
				if e != nil {
					t.Fatal(e)
				}
				pc := uint32(bytes.Index(m.Code[1].BodyBytes, []byte{0xfb, 2, 0, 0})) + m.Code[1].LocalDeclBytes
				if len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != pc {
					t.Fatalf("wrong null trap PC: %+v want%d", trap.Frames, pc)
				}
			}
		})
	}
}

func TestCompactNativeGCReadAvoidsHelper(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), compactPublicRootsModule())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	values, e := in.InvokeValues(context.Background(), "new", ValueI32(91))
	if e != nil {
		t.Fatal(e)
	}
	defer in.ReleaseGCRef(values[0].GCRef())
	if e = in.SetGlobalValue("root", values[0]); e != nil {
		t.Fatal(e)
	}
	index := in.ctrl[coreruntime.HostCtrlImportIndexOffset:]
	const sentinel = 0x81234567
	binary.LittleEndian.PutUint32(index, sentinel)
	got, e := in.Invoke("read")
	if e != nil || len(got) != 1 || got[0] != 91 {
		t.Fatal(got, e)
	}
	if binary.LittleEndian.Uint32(index) != sentinel {
		t.Fatal("scalar read invoked helper")
	}
}
